# Back up or restore one Motty profile's settings and named local terminals.
# Requires Git with a GitHub credential that can write Gists.
[CmdletBinding()]
param(
    [ValidateSet('Backup', 'Verify', 'Restore')][string]$Action = 'Backup',
    [string]$Database,
    [string]$Motty = (Join-Path $PSScriptRoot 'motty.exe'),
    [string]$RecoveryFile = (Join-Path $env:LOCALAPPDATA 'MottySettingsBackup\recovery.json')
)
$ErrorActionPreference = 'Stop'
$utf8 = [Text.UTF8Encoding]::new($false)
$filename = 'motty-settings.encrypted.json'
$api = 'https://api.github.com'
$Motty = (Resolve-Path -LiteralPath $Motty).Path
$RecoveryFile = [IO.Path]::GetFullPath($RecoveryFile)
$directory = [IO.Path]::GetDirectoryName($RecoveryFile)
[void][IO.Directory]::CreateDirectory($directory)

function Save-Recovery($profileData) {
    # Grant the current user access before writing any key bytes. Replacing an
    # existing recovery file is atomic, including when the first Gist is created.
    $stage = Join-Path $directory ([Guid]::NewGuid().ToString('N') + '.tmp')
    [IO.File]::Create($stage).Dispose()
    try {
        $acl = Get-Acl -LiteralPath $stage
        $acl.SetAccessRuleProtection($true, $false)
        $user = [Security.Principal.WindowsIdentity]::GetCurrent().User
        $acl.SetAccessRule([Security.AccessControl.FileSystemAccessRule]::new($user, 'FullControl', 'Allow'))
        Set-Acl -LiteralPath $stage -AclObject $acl
        [IO.File]::WriteAllText($stage, ($profileData | ConvertTo-Json -Compress), $utf8)
        if ([IO.File]::Exists($RecoveryFile)) { [IO.File]::Replace($stage, $RecoveryFile, [Management.Automation.Language.NullString]::Value) }
        else { [IO.File]::Move($stage, $RecoveryFile) }
    } finally { if ([IO.File]::Exists($stage)) { [IO.File]::Delete($stage) } }
}

function Invoke-GitHub([string]$method, [string]$route, $body) {
    $options = @{Method=$method; Uri=($api + $route); Headers=$headers; MaximumRedirection=0; TimeoutSec=90}
    if ($null -ne $body) {
        $options.ContentType = 'application/json; charset=utf-8'
        $options.Body = $utf8.GetBytes(($body | ConvertTo-Json -Depth 5 -Compress))
    }
    Invoke-RestMethod @options
}

function Invoke-Settings([string]$operation, [string]$file) {
    # Piping stdout also waits for a Windows GUI-subsystem executable.
    $output = $profileData.key | & $Motty settings-backup $operation $Database $file | Out-String
    if ($LASTEXITCODE -ne 0) { throw "Motty settings $operation failed." }
    if ($output.Trim()) { Write-Output $output.Trim() }
}

function Read-Remote {
    if ($profileData.gist -notmatch '^[0-9a-f]{20,64}$') { throw 'Invalid Gist ID in the recovery file.' }
    $item = Invoke-GitHub 'Get' ('/gists/' + $profileData.gist) $null
    if ($item.public -ne $false -or $item.owner.login -ne $profileData.owner -or
        $item.description -ne $profileData.description) { throw 'The Gist owner, visibility or identity changed.' }
    $files = @($item.files.PSObject.Properties)
    if ($files.Count -ne 1 -or $files[0].Name -ne $filename) { throw 'Unexpected Gist file list.' }
    $remoteFile = $files[0].Value
    if ($remoteFile.truncated -or !$remoteFile.content) { throw 'Incomplete encrypted Gist file.' }
    [IO.File]::WriteAllText($download, $remoteFile.content, $utf8)
}

$lock = [IO.File]::Open(($RecoveryFile + '.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
$oldPrompt = $env:GIT_TERMINAL_PROMPT
$oldInteractive = $env:GCM_INTERACTIVE
$scratch = Join-Path ([IO.Path]::GetTempPath()) ('motty-settings-' + [Guid]::NewGuid().ToString('N'))
try {
    [void][IO.Directory]::CreateDirectory($scratch)
    $download = Join-Path $scratch 'remote.json'
    $export = Join-Path $scratch 'export.json'
    $env:GIT_TERMINAL_PROMPT = '0'
    $env:GCM_INTERACTIVE = 'never'
    $request = "protocol=https`nhost=github.com`n`n"
    $credential = $request | git -c credential.interactive=false credential fill
    if ($LASTEXITCODE -ne 0) { throw 'Sign in to GitHub with Git Credential Manager first.' }
    $line = $credential | Where-Object { $_.StartsWith('password=') } | Select-Object -First 1
    if (!$line) { throw 'Git did not return a GitHub credential.' }
    $headers = @{'Authorization'=('Bearer ' + $line.Substring(9)); 'User-Agent'='Motty-settings-backup'; 'Accept'='application/vnd.github+json'}
    $account = (Invoke-GitHub 'Get' '/user' $null).login
    if ([IO.File]::Exists($RecoveryFile)) {
        $profileData = [IO.File]::ReadAllText($RecoveryFile) | ConvertFrom-Json
        if ($profileData.owner -ne $account -or !$profileData.key -or !$profileData.description) { throw 'Recovery file and GitHub account do not match.' }
    } else {
        if ($Action -ne 'Backup') { throw 'Supply the recovery file from the original computer.' }
        if (!$Database) { $Database = Join-Path $env:APPDATA 'motty\config.db' }
        $Database = (Resolve-Path -LiteralPath $Database).Path
        $bytes = [byte[]]::new(32)
        $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
        $profileData = [pscustomobject]@{owner=$account; description=('Motty settings ' + [Guid]::NewGuid().ToString('N')); gist=''; key=[Convert]::ToBase64String($bytes); database=$Database}
        Save-Recovery $profileData
    }
    if (!$Database) { $Database = $profileData.database }
    $Database = [IO.Path]::GetFullPath($Database)
    if ($Action -ne 'Restore' -and ![IO.File]::Exists($Database)) { throw 'The selected configuration database does not exist.' }
    if (!$profileData.gist) {
        # Recover from an interrupted create before allocating a second Gist.
        for ($page = 1; ; $page++) {
            $items = @(Invoke-GitHub 'Get' ("/gists?per_page=100&page=$page") $null | ForEach-Object { $_ })
            $previous = $items | Where-Object { $_.description -eq $profileData.description -and $_.owner.login -eq $account } | Select-Object -First 1
            if ($previous) { $profileData.gist = $previous.id; Save-Recovery $profileData; break }
            if ($items.Count -lt 100) { break }
        }
    }
    if ($profileData.gist) {
        Read-Remote
        if ($Action -eq 'Restore') { Invoke-Settings 'restore' $download; return }
        if ($Action -eq 'Verify') { Invoke-Settings 'verify' $download; return }
        Invoke-Settings 'check' $download | Out-Null
    } elseif ($Action -ne 'Backup') { throw 'The recovery file has no previous Gist.' }

    Invoke-Settings 'export' $export
    if ((Get-Item -LiteralPath $export).Length -gt 900000) { throw 'Encrypted settings exceed the supported Gist size.' }
    $ciphertext = [IO.File]::ReadAllText($export)
    $body = @{files=@{$filename=@{content=$ciphertext}}}
    if ($profileData.gist) {
        Invoke-GitHub 'Patch' ('/gists/' + $profileData.gist) $body | Out-Null
    } else {
        $body.public = $false
        $body.description = $profileData.description
        $created = Invoke-GitHub 'Post' '/gists' $body
        $profileData.gist = $created.id
        Save-Recovery $profileData
    }
    Read-Remote
    if ([IO.File]::ReadAllText($download) -cne $ciphertext) { throw 'GitHub returned different encrypted bytes.' }
    Invoke-Settings 'verify' $download
    Write-Output ("Verified secret Gist " + $profileData.gist)
} finally {
    $env:GIT_TERMINAL_PROMPT = $oldPrompt
    $env:GCM_INTERACTIVE = $oldInteractive
    foreach ($file in @('remote.json', 'export.json')) { [IO.File]::Delete((Join-Path $scratch $file)) }
    if ([IO.Directory]::Exists($scratch)) { [IO.Directory]::Delete($scratch) }
    $lock.Dispose()
}
