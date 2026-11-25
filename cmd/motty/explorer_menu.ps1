$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$root = $env:MOTTY_MENU_ROOT
$menu = Join-Path $root ($env:MOTTY_MENU_BASE + '.explorer')
$layout = Join-Path $menu 'AppxManifest.xml'
$install = $env:MOTTY_MENU_ACTION -eq 'register'
if ($install) {
    $identity = ([xml][IO.File]::ReadAllText($layout)).Package.Identity
}

$manager = [Windows.Management.Deployment.PackageManager,Windows.Management.Deployment,ContentType=WindowsRuntime]::new()
foreach ($pkg in Get-AppxPackage -Name $env:MOTTY_MENU_ID) {
    $external = $manager.FindPackageForUser('', $pkg.PackageFullName).EffectiveExternalPath
    if ($external -ne $root) {
        if ($install) { throw "Unregister the existing menu in $external before moving it to $root." }
        continue
    }
    if (!$install) {
        Remove-AppxPackage -Package $pkg.PackageFullName
    } elseif ($pkg.Status -eq 'Ok' -and $pkg.Version -eq $identity.Version -and
              $pkg.Publisher -eq $identity.Publisher -and $pkg.Architecture -eq $identity.ProcessorArchitecture) {
        # Explorer may be using the DLL. An already registered build needs no deployment.
        return
    }
}
if (!$install) { return }

$devMode = Get-ItemPropertyValue 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock' -Name AllowDevelopmentWithoutDevLicense -ErrorAction SilentlyContinue
if ($env:MOTTY_MENU_BASE -eq 'motty-dev' -and $devMode -eq 1) {
    Add-AppxPackage -Register -Path $layout -ExternalLocation $root
} else {
    Add-AppxPackage -Path (Join-Path $menu 'MottyExplorer.msix') -ExternalLocation $root
}
if (!(Get-AppxPackage -Name $env:MOTTY_MENU_ID)) { throw 'Explorer package registration was not found.' }
