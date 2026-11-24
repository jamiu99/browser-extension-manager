param([Parameter(Mandatory = $true)][string]$InputPath)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)

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
        Where-Object { $_.Current.ProcessId -in $processIds }
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

function Close-ExtensionWindow($Window) {
    if ($Window) {
        try { $Window.GetCurrentPattern([Windows.Automation.WindowPattern]::Pattern).Close() } catch { }
    }
}

function Open-ExtensionPage($Browser) {
    Initialize-BrowserAutomation
    $previous = @(Get-BrowserWindows $Browser | ForEach-Object { $_.Current.NativeWindowHandle })
    $arguments = @('--new-window', 'about:blank')
    if ($Browser.Profile) { $arguments = @("--profile-directory=`"$($Browser.Profile)`"") + $arguments }
    $null = Start-Process -FilePath $Browser.Exe -ArgumentList $arguments
    $deadline = [DateTime]::UtcNow.AddSeconds(9)
    try {
        do {
            Start-Sleep -Milliseconds 160
            $candidates = @(Get-BrowserWindows $Browser | Where-Object {
                $_.Current.NativeWindowHandle -notin $previous -and -not $_.Current.IsOffscreen
            })
            if ($candidates.Count -ne 1) { continue }
            $window = $candidates[0]
            $address = Find-Control $window ([Windows.Automation.ControlType]::Edit) | Where-Object {
                $_.Current.AutomationId -eq 'view_1017' -or $_.Current.Name -match '地址和搜索栏|地址栏|^Address and search bar$|^Search or enter address$'
            } | Select-Object -First 1
            if ($address) { break }
        } until ([DateTime]::UtcNow -ge $deadline)
        if (-not $address) { throw "无法打开 $($Browser.Name) 的扩展管理页。" }

        if (-not (Open-ManagementMenuPage $window)) {
            $handle = [IntPtr]$window.Current.NativeWindowHandle
            $shell = New-Object -ComObject WScript.Shell
            $null = $shell.AppActivate($window.Current.ProcessId)
            $null = [ExtensionManager.NativeWindow]::SetForegroundWindow($handle)
            $address.SetFocus()
            if ([ExtensionManager.NativeWindow]::GetForegroundWindow() -ne $handle) {
                throw "$($Browser.Name) 的临时窗口未能获得焦点。"
            }
            $address.GetCurrentPattern([Windows.Automation.ValuePattern]::Pattern).SetValue($Browser.Address)
            $shell.SendKeys('{ENTER}')
        }

        $page = @{ Window = $window; Address = $address; BaseAddress = $Browser.Address }
        $deadline = [DateTime]::UtcNow.AddSeconds(8)
        do {
            Start-Sleep -Milliseconds 130
            $document = Find-Control $window ([Windows.Automation.ControlType]::Document) | Where-Object {
                $_.Current.Name -match '扩展|Extensions'
            } | Select-Object -First 1
            if ($document -and (Get-PageAddress $page).TrimEnd('/') -eq $Browser.Address) {
                $page.Document = $document
                return $page
            }
        } until ([DateTime]::UtcNow -ge $deadline)
        throw "$($Browser.Name) 的扩展管理页加载超时。"
    } catch {
        Close-ExtensionWindow $window
        throw
    }
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

function Find-ReloadButton($Document, $ExtensionId, $Name) {
    $walker = [Windows.Automation.TreeWalker]::RawViewWalker
    $nameMatches = @()
    $buttons = Find-Control $Document ([Windows.Automation.ControlType]::Button) | Where-Object {
        $_.Current.Name -match '^(Reload( extension)?|重新加载(扩展程序)?|重新载入|重载)$'
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
    throw '扩展列表加载超时。'
}

function Test-ExtensionVersion($Container, $Version) {
    $expected = [regex]::Escape($Version)
    $texts = Find-Control $Container ([Windows.Automation.ControlType]::Text) | ForEach-Object {
        $_.Current.Name -replace '\s',''
    }
    return [bool]($texts -match "^(?:(?:Version|版本)[:：]?)?$expected$")
}

function Update-BrowserExtension($Page, $Entry) {
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    do {
        Update-PageDocument $Page
        $match = Find-ReloadButton $Page.Document $Entry.ID $Entry.Name
        if ($match) { break }
        Start-Sleep -Milliseconds 150
    } until ([DateTime]::UtcNow -ge $deadline)
    if (-not $match) { throw '浏览器未提供此插件的重载按钮。' }

    if (-not $match.Verified) {
        $exact = Open-CardDetails $Page $match.Card $Entry.ID
        Return-ToExtensionList $Page
        if (-not $exact) { throw '浏览器中的插件身份无法确认。' }
        $match = Find-ReloadButton $Page.Document $Entry.ID $Entry.Name
        if (-not $match) { throw '浏览器未提供此插件的重载按钮。' }
    }
    $match.Button.GetCurrentPattern([Windows.Automation.InvokePattern]::Pattern).Invoke()
    Start-Sleep -Milliseconds 350

    $deadline = [DateTime]::UtcNow.AddSeconds(6)
    do {
        try {
            Update-PageDocument $Page
            $errors = Find-Control $Page.Document ([Windows.Automation.ControlType]::Text) | Where-Object {
                $_.Current.Name -match '^(Failed to load extension|Error loading extension|加载扩展程序失败|无法加载扩展程序|重新加载失败)$'
            } | Select-Object -First 1
            if ($errors) { throw '浏览器报告插件加载失败，请检查更新包。' }
            $reloaded = Find-ReloadButton $Page.Document $Entry.ID $Entry.Name
            if ($reloaded -and $reloaded.Verified -and (Test-ExtensionVersion $reloaded.Card $Entry.Version)) { return }
        } catch [Windows.Automation.ElementNotAvailableException] { }
        Start-Sleep -Milliseconds 160
    } until ([DateTime]::UtcNow -ge $deadline)

    if ($reloaded -and (Open-CardDetails $Page $reloaded.Card $Entry.ID)) {
        Update-PageDocument $Page
        $confirmed = Test-ExtensionVersion $Page.Document $Entry.Version
        Return-ToExtensionList $Page
        if ($confirmed) { return }
    } else {
        Return-ToExtensionList $Page
    }
    throw "浏览器尚未确认版本 $($Entry.Version)。"
}

function Invoke-BrowserReload($Targets) {
    foreach ($browser in $Targets) {
        $page = $null
        try {
            $page = Open-ExtensionPage $browser
            foreach ($entry in $browser.Extensions) {
                try {
                    Update-BrowserExtension $page $entry
                    "$($browser.Name) · 已重载 $($entry.Name) $($entry.Version)"
                } catch {
                    "$($browser.Name) · $($entry.Name) 重载未完成。$($_.Exception.Message)"
                }
            }
        } catch {
            "$($browser.Name) · 重载未完成。$($_.Exception.Message) 可在管理器里点「重载所选」重试。"
        } finally {
            Close-ExtensionWindow $page.Window
        }
    }
}

$request = [IO.File]::ReadAllText($InputPath) | ConvertFrom-Json
if ($request.Install) {
    Open-BrowserInstall $request.Targets
} else {
    Invoke-BrowserReload $request.Targets
}
