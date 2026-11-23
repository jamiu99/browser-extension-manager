param([Parameter(Mandatory = $true)][string]$InputPath)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)

function Read-ExtensionManifest($Directory) {
    [IO.File]::ReadAllText((Join-Path $Directory 'manifest.json')) | ConvertFrom-Json
}

function Get-ExtensionDisplayName($Directory, $Manifest) {
    if ($Manifest.name -notmatch '^__MSG_([a-zA-Z0-9_@]+)__$') { return $Manifest.name }
    $key = $Matches[1]
    $culture = [Globalization.CultureInfo]::CurrentUICulture
    $locales = @($culture.Name.Replace('-', '_'), $culture.TwoLetterISOLanguageName, $Manifest.default_locale)
    foreach ($locale in ($locales | Where-Object { $_ -match '^[a-zA-Z0-9_-]+$' } | Select-Object -Unique)) {
        $file = Join-Path $Directory "_locales\$locale\messages.json"
        if ([IO.File]::Exists($file)) {
            $messages = [IO.File]::ReadAllText($file) | ConvertFrom-Json
            $name = $messages.PSObject.Properties[$key].Value.message
            if ($name) { return $name }
        }
    }
    return [IO.Path]::GetFileName($Directory)
}

function Initialize-BrowserAutomation {
    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
    if ('ExtensionManager.NativeWindow' -as [type]) { return }
    Add-Type -Namespace ExtensionManager -Name NativeWindow -MemberDefinition @'
    [System.Runtime.InteropServices.DllImport("user32.dll")]
    public static extern System.IntPtr GetForegroundWindow();

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(System.IntPtr handle);
'@
}

function Get-BrowserWindows($Browser) {
    $name = [IO.Path]::GetFileNameWithoutExtension($Browser.Exe)
    $processIds = @(Get-Process -Name $name -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id)
    $condition = [Windows.Automation.PropertyCondition]::new(
        [Windows.Automation.AutomationElement]::ClassNameProperty, 'Chrome_WidgetWin_1'
    )
    [Windows.Automation.AutomationElement]::RootElement.FindAll([Windows.Automation.TreeScope]::Children, $condition) |
        Where-Object { $_.Current.ProcessId -in $processIds -and -not $_.Current.IsOffscreen }
}

function Find-Control($Root, $Type) {
    $condition = [Windows.Automation.PropertyCondition]::new(
        [Windows.Automation.AutomationElement]::ControlTypeProperty, $Type
    )
    $Root.FindAll([Windows.Automation.TreeScope]::Descendants, $condition)
}

function Open-ManagementMenuPage($Window) {
    $button = Find-Control $Window ([Windows.Automation.ControlType]::Button) | Where-Object {
        $_.Current.Name -match '^(Extensions|扩展|扩展程序)$'
    } | Select-Object -First 1
    if (-not $button) { return $false }
    try {
        $button.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
        $condition = [Windows.Automation.OrCondition]::new([Windows.Automation.Condition[]]@(
            [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::NameProperty, 'Manage extensions'),
            [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::NameProperty, '管理扩展'),
            [Windows.Automation.PropertyCondition]::new([Windows.Automation.AutomationElement]::NameProperty, '管理扩展程序')
        ))
        $deadline = [DateTime]::UtcNow.AddSeconds(2)
        do {
            $item = $Window.FindFirst([Windows.Automation.TreeScope]::Descendants, $condition)
            if ($item) {
                $item.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
                return $true
            }
            Start-Sleep -Milliseconds 120
        } until ([DateTime]::UtcNow -ge $deadline)
    } catch { }
    return $false
}

function Open-ExtensionPage($Browser) {
    Initialize-BrowserAutomation
    $previous = @(Get-BrowserWindows $Browser | ForEach-Object { $_.Current.NativeWindowHandle })
    Start-Process -FilePath $Browser.Exe -ArgumentList @('--new-window', 'about:blank')
    $deadline = [DateTime]::UtcNow.AddSeconds(6)
    do {
        Start-Sleep -Milliseconds 150
        $window = Get-BrowserWindows $Browser | Where-Object { $_.Current.NativeWindowHandle -notin $previous } | Select-Object -First 1
        if ($window) {
            $address = Find-Control $window ([Windows.Automation.ControlType]::Edit) | Where-Object {
                $_.Current.AutomationId -eq 'view_1017' -or $_.Current.Name -match '地址和搜索栏|地址栏|^Address and search bar$|^Search or enter address$'
            } | Select-Object -First 1
            if ($address) { break }
        }
    } until ([DateTime]::UtcNow -ge $deadline)
    if (-not $address) { throw "没有找到 $($Browser.Name) 地址栏，请打开 $($Browser.Address)。" }

    if (-not (Open-ManagementMenuPage $window)) {
        $handle = [IntPtr]$window.Current.NativeWindowHandle
        $shell = New-Object -ComObject WScript.Shell
        $null = $shell.AppActivate($window.Current.ProcessId)
        $null = [ExtensionManager.NativeWindow]::SetForegroundWindow($handle)
        $address.SetFocus()
        if ([ExtensionManager.NativeWindow]::GetForegroundWindow() -ne $handle) {
            throw "$($Browser.Name) 窗口没有获得焦点，请打开 $($Browser.Address)。"
        }
        $address.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern).SetValue($Browser.Address)
        $shell.SendKeys('{ENTER}')
    }

    $deadline = [DateTime]::UtcNow.AddSeconds(4)
    do {
        Start-Sleep -Milliseconds 130
        $document = Find-Control $window ([Windows.Automation.ControlType]::Document) | Where-Object {
            $_.Current.Name -match '扩展|Extensions'
        } | Select-Object -First 1
        $location = $address.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern).Current.Value
        if ($document -and $location.TrimEnd('/') -eq $Browser.Address) {
            return @{ Window = $window; Document = $document; Address = $address; BaseAddress = $Browser.Address }
        }
    } until ([DateTime]::UtcNow -ge $deadline)
    throw "$($Browser.Name) 尚未到达扩展页，请打开 $($Browser.Address)。"
}

function Open-BrowserInstall($Targets) {
    foreach ($browser in $Targets) {
        try {
            $page = Open-ExtensionPage $browser
            $button = Find-Control $page.Document ([Windows.Automation.ControlType]::Button) | Where-Object {
                $_.Current.Name -match '^Load unpacked$|^加载.*(?:解压|未打包)'
            } | Select-Object -First 1
            if ($button) {
                $button.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
                "$($browser.Name) · 请粘贴目录并确认选择。"
            } else {
                "$($browser.Name) · 在扩展页开启开发人员模式，并加载已解压的插件目录。"
            }
        } catch { $_.Exception.Message }
    }
}

function Get-ExtensionId($Entry) {
    if ($Entry.id) { return $Entry.id }
    $manifest = Read-ExtensionManifest $Entry.path
    if ($manifest.key) {
        $bytes = [Convert]::FromBase64String($manifest.key)
    } else {
        $path = $Entry.path
        if ($path -match '^[a-z]:') { $path = $path.Substring(0, 1).ToUpperInvariant() + $path.Substring(1) }
        $bytes = [Text.Encoding]::Unicode.GetBytes($path)
    }
    $sha = [Security.Cryptography.SHA256]::Create()
    try { $hex = [BitConverter]::ToString($sha.ComputeHash($bytes), 0, 16).Replace('-', '') }
    finally { $sha.Dispose() }
    return -join ($hex.ToCharArray() | ForEach-Object { [char](97 + [Convert]::ToInt32([string]$_, 16)) })
}

function Find-ReloadButton($Document, $ExtensionId, $Name) {
    $walker = [Windows.Automation.TreeWalker]::RawViewWalker
    $nameMatches = @()
    $buttons = Find-Control $Document ([Windows.Automation.ControlType]::Button) | Where-Object {
        $_.Current.Name -match '^(Reload|重新加载|重新载入|重载)$'
    }
    foreach ($button in $buttons) {
        $card = $walker.GetParent($button)
        while ($card -and $card.Current.ControlType -ne [Windows.Automation.ControlType]::Document) {
            $cardButtons = @(Find-Control $card ([Windows.Automation.ControlType]::Button) | ForEach-Object { $_.Current.Name })
            $details = @($cardButtons -match '^(Details|详细信息|详情)$')
            if ($details.Count -gt 1) { break }
            $texts = @(Find-Control $card ([Windows.Automation.ControlType]::Text) | ForEach-Object { $_.Current.Name })
            $ids = @($texts | ForEach-Object {
                if ($_ -match '^(?:(?:扩展程序\s*|扩展\s*)?ID\s*[:：]\s*)?([a-p]{32})$') { $Matches[1] }
            })
            if ($ids.Count -gt 1) { break }
            if ($ids.Count -eq 1) {
                if ($ids[0] -eq $ExtensionId) { return @{ Button = $button; Card = $card; Verified = $true } }
                break
            }
            if ($details.Count -eq 1 -and $texts -ccontains $Name) {
                $nameMatches += @{ Button = $button; Card = $card }
                break
            }
            $card = $walker.GetParent($card)
        }
    }
    if ($nameMatches.Count -eq 1) { return $nameMatches[0] }
}

function Get-PageAddress($Page) {
    $Page.Address.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern).Current.Value
}

function Update-PageDocument($Page) {
    $Page.Document = Find-Control $Page.Window ([Windows.Automation.ControlType]::Document) | Select-Object -First 1
}

function Open-CardDetails($Page, $Card, $Id) {
    $details = Find-Control $Card ([Windows.Automation.ControlType]::Button) | Where-Object {
        $_.Current.Name -match '^(Details|详细信息|详情)$'
    } | Select-Object -First 1
    if (-not $details) { return $false }
    $details.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
    $deadline = [DateTime]::UtcNow.AddSeconds(2)
    do {
        Start-Sleep -Milliseconds 120
        $location = Get-PageAddress $Page
        if ($location -match '[?&]id=([a-p]{32})') { return $Matches[1] -eq $Id }
    } until ([DateTime]::UtcNow -ge $deadline)
    return $false
}

function Return-ToExtensionList($Page) {
    if ((Get-PageAddress $Page) -notmatch '[?&]id=') { return }
    $back = Find-Control $Page.Window ([Windows.Automation.ControlType]::Button) | Where-Object {
        $_.Current.AutomationId -eq 'view_1001' -or $_.Current.Name -match '^(Back|返回)$'
    } | Select-Object -First 1
    $back.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
    $deadline = [DateTime]::UtcNow.AddSeconds(2)
    do {
        Start-Sleep -Milliseconds 120
        if ((Get-PageAddress $Page).TrimEnd('/') -eq $Page.BaseAddress) {
            Update-PageDocument $Page
            return
        }
    } until ([DateTime]::UtcNow -ge $deadline)
    throw '浏览器还在插件详情中。请返回扩展列表后重试。'
}

function Update-BrowserExtension($Page, $Entry) {
    $manifest = Read-ExtensionManifest $Entry.path
    $name = Get-ExtensionDisplayName $Entry.path $manifest
    $id = Get-ExtensionId $Entry
    $match = Find-ReloadButton $Page.Document $id $name
    if (-not $match) {
        Start-Sleep -Milliseconds 400
        $match = Find-ReloadButton $Page.Document $id $name
    }
    if (-not $match) { return $false }
    if (-not $match.Verified) {
        $exact = Open-CardDetails $Page $match.Card $id
        Return-ToExtensionList $Page
        if (-not $exact) { return $false }
        $match = Find-ReloadButton $Page.Document $id $name
        if (-not $match) { return $false }
    }
    $match.Button.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
    Start-Sleep -Milliseconds 450
    $reloaded = Find-ReloadButton $Page.Document $id $name
    if (-not $reloaded) { return $false }
    $confirmed = $false
    if (Open-CardDetails $Page $reloaded.Card $id) {
        $version = [regex]::Escape([string]$manifest.version)
        $deadline = [DateTime]::UtcNow.AddSeconds(2)
        do {
            $texts = @(Find-Control $Page.Window ([Windows.Automation.ControlType]::Text) | ForEach-Object {
                $_.Current.Name -replace '\s',''
            })
            if ($texts -match "^(?:(?:Version|版本)[:：]?)?$version$") {
                $confirmed = $true
                break
            }
            Start-Sleep -Milliseconds 130
        } until ([DateTime]::UtcNow -ge $deadline)
    }
    Return-ToExtensionList $Page
    return $confirmed
}

function Invoke-BrowserReload($Targets, $Items) {
    foreach ($browser in $Targets) {
        try {
            $page = Open-ExtensionPage $browser
            $complete = $true
            foreach ($entry in $Items) {
                $name = Get-ExtensionDisplayName $entry.path (Read-ExtensionManifest $entry.path)
                $applied = $false
                try { $applied = Update-BrowserExtension $page $entry } catch { }
                if ($applied) {
                    "$($browser.Name) · 已重载 $name"
                } else {
                    $complete = $false
                    "$($browser.Name) · 请确认 $name 已加载，并在扩展卡片上点「重新加载」。"
                }
            }
            if ($complete) { $page.Window.GetCurrentPattern([Windows.Automation.WindowPattern]::Pattern).Close() }
        } catch { "$($browser.Name) · $($_.Exception.Message)" }
    }
}

$request = [IO.File]::ReadAllText($InputPath) | ConvertFrom-Json
if ($request.Install) {
    Open-BrowserInstall $request.Targets
} else {
    Invoke-BrowserReload $request.Targets $request.Items
}
