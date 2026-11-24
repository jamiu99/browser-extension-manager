param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$Archive
)
$ErrorActionPreference = 'Stop'
$exe = (Resolve-Path -LiteralPath $Path).Path
$bytes = [IO.File]::ReadAllBytes($exe)
$pe = [BitConverter]::ToInt32($bytes, 0x3c)
if ([BitConverter]::ToUInt32($bytes, $pe) -ne 0x4550 -or
    [BitConverter]::ToUInt16($bytes, $pe + 24 + 68) -ne 2) {
    throw 'Expected a Windows GUI executable'
}
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class ExecutableResources {
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern IntPtr LoadLibraryEx(string path, IntPtr file, uint flags);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern IntPtr FindResource(IntPtr module, IntPtr resource, IntPtr type);
    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool FreeLibrary(IntPtr module);
}
'@
$module = [ExecutableResources]::LoadLibraryEx($exe, [IntPtr]::Zero, 2)
if ($module -eq [IntPtr]::Zero) { throw 'Could not map executable resources' }
try {
    if ([ExecutableResources]::FindResource($module, [IntPtr]3, [IntPtr]14) -eq [IntPtr]::Zero) {
        throw 'Missing application icon group'
    }
} finally {
    $null = [ExecutableResources]::FreeLibrary($module)
}
Write-Output 'Verified Windows GUI subsystem and application icon group.'
$zip = [IO.Compression.ZipFile]::OpenRead((Resolve-Path -LiteralPath $Archive).Path)
try {
    foreach ($name in 'extension-manager.exe', 'extension-manager-cli.exe', 'release.json', 'update-feed.md', 'browser-configuration.md', 'THIRD-PARTY-NOTICES.txt') {
        if (-not $zip.GetEntry($name)) { throw "Missing $name in Windows ZIP" }
    }
} finally {
    $zip.Dispose()
}
Write-Output 'Verified Windows ZIP contents.'
