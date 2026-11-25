# Build the optional Windows 11 Explorer integration beside an existing Motty
# executable. The DLL uses the static CRT and needs no redistributable.
[CmdletBinding()]
param(
    [ValidateSet('Dev', 'Release')][string]$Variant = 'Dev',
    [ValidateSet('x64', 'arm64')][string]$Architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant(),
    [string]$AppDirectory = (Join-Path $PSScriptRoot '..\..'),
    [string]$Publisher,
    [string]$CertificateThumbprint,
    [version]$Version = '1.0.0.0'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$appDir = (Resolve-Path -LiteralPath $AppDirectory).Path
if ($Variant -eq 'Dev') {
    $base = 'motty-dev'
    $product = 'Motty Dev'
    $identity = 'Motty.Dev.ExplorerMenu'
    $classId = '961e496f-9e5f-4a8c-be80-f9c9133c64d8'
    if (!$Publisher) { $Publisher = 'CN=Motty Dev Local Explorer Menu' }
} else {
    $base = 'motty'
    $product = 'Motty'
    $identity = 'Motty.ExplorerMenu'
    $classId = '646da3c0-c661-42ef-b298-bb1e2c7abd21'
    if (!$Publisher) { $Publisher = 'CN=Motty' }
}
if (!(Test-Path -LiteralPath (Join-Path $appDir "$base.exe") -PathType Leaf)) {
    throw "Build $base.exe in $appDir first."
}
$work = Join-Path $root "build\explorermenu\$Variant-$Architecture"
$out = Join-Path $appDir "$base.explorer"
[void][IO.Directory]::CreateDirectory($work)
[void][IO.Directory]::CreateDirectory($out)

if ($env:VSCMD_ARG_TGT_ARCH -ne $Architecture -or !(Get-Command cl.exe -ErrorAction SilentlyContinue)) {
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
    $component = if ($Architecture -eq 'arm64') { 'Microsoft.VisualStudio.Component.VC.Tools.ARM64' } else { 'Microsoft.VisualStudio.Component.VC.Tools.x86.x64' }
    $vs = & $vswhere -latest -prerelease -products '*' -requires $component -property installationPath
    if (!$vs) { throw "Install Visual Studio C++ build tools for $Architecture and the Windows 11 SDK." }
    $hostArch = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant().Replace('x64', 'amd64')
    & (Join-Path $vs 'Common7\Tools\Launch-VsDevShell.ps1') -SkipAutomaticLocation -HostArch $hostArch -Arch ($Architecture.Replace('x64', 'amd64'))
}
if ($CertificateThumbprint) {
    if ($CertificateThumbprint -notmatch '^[0-9a-fA-F]{40}$') { throw 'Invalid signing certificate thumbprint.' }
    $certificate = Get-Item -LiteralPath ("Cert:\CurrentUser\My\" + $CertificateThumbprint)
    $Publisher = $certificate.Subject
}

$utf8 = [Text.UTF8Encoding]::new($false)
$config = @"
#define MOTTY_MENU_CLSID "$classId"
#define MOTTY_MENU_PRODUCT L"$product"
#define MOTTY_MENU_EXE L"$base.exe"
"@
[IO.File]::WriteAllText((Join-Path $work 'MenuConfig.h'), $config, $utf8)
& cl.exe /nologo /LD /MT /EHsc /std:c++17 /utf-8 /O2 /W4 /guard:cf "/I$work" "/Fo$work\OpenHere.obj" (Join-Path $PSScriptRoot 'OpenHere.cpp') /link /guard:cf "/DEF:$PSScriptRoot\OpenHere.def" "/OUT:$work\OpenHere.dll" "/IMPLIB:$work\OpenHere.lib" ole32.lib shlwapi.lib uuid.lib runtimeobject.lib
if ($LASTEXITCODE) { throw 'Cannot build the Explorer command DLL.' }
Copy-Item -LiteralPath (Join-Path $work 'OpenHere.dll') -Destination $out -Force
Copy-Item -LiteralPath (Join-Path $root 'assets\icon-tile.png') -Destination (Join-Path $out 'Logo.png') -Force

Add-Type -AssemblyName System.Drawing
$source = [Drawing.Image]::FromFile((Join-Path $out 'Logo.png'))
try {
    $bitmap = [Drawing.Bitmap]::new($source, 48, 48)
    try {
        $icon = [Drawing.Icon]::FromHandle($bitmap.GetHicon())
        $file = [IO.File]::Create((Join-Path $out 'menu.ico'))
        try { $icon.Save($file) } finally { $file.Dispose(); $icon.Dispose() }
    } finally { $bitmap.Dispose() }
} finally { $source.Dispose() }

$template = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'AppxManifest.xml.in'))
$manifest = $template.Replace('@IDENTITY@', $identity).Replace('@VERSION@', $Version.ToString(4)).Replace('@ARCH@', $Architecture).Replace('@PRODUCT@', $product).Replace('@BASENAME@', $base).Replace('@CLSID@', $classId)
$layout = Join-Path $out 'AppxManifest.xml'
[IO.File]::WriteAllText($layout, $manifest.Replace('@PUBLISHER@', [Security.SecurityElement]::Escape($Publisher)), $utf8)
$package = Join-Path $out 'MottyExplorer.msix'
$map = Join-Path $work 'mapping.txt'
[IO.File]::WriteAllLines($map, @('[Files]', ('"{0}" "AppxManifest.xml"' -f $layout)), [Text.Encoding]::Unicode)
& makeappx.exe pack /o /nv /f $map /p $package
if ($LASTEXITCODE) { throw 'Cannot package the Explorer command.' }
if ($CertificateThumbprint) {
    & signtool.exe sign /fd SHA256 /s My /sha1 $CertificateThumbprint (Join-Path $out 'OpenHere.dll')
    if ($LASTEXITCODE) { throw 'Cannot sign the Explorer command DLL.' }
    & signtool.exe sign /fd SHA256 /s My /sha1 $CertificateThumbprint $package
    if ($LASTEXITCODE) { throw 'Cannot sign the Explorer package.' }
    [IO.File]::WriteAllBytes((Join-Path $out 'Publisher.cer'), $certificate.RawData)
}
Write-Output $package
