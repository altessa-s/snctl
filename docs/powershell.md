# PowerShell on Windows

Back to the [README](../README.md).

These jobs target PowerShell 7.2 or later. Save the named scripts together in
a directory such as `C:\Jobs\Supernote`. Run them from a PowerShell prompt
before installing a shortcut or scheduled task. The scripts were run with
PowerShell 7.4 on Windows 11 (arm64) against a private-cloud bench: from a
prompt, from an Explorer Send To shortcut and from Task Scheduler.

## Contents

- [Install and completion](#install-and-completion)
- [What is different on Windows](#what-is-different-on-windows)
- [Ground rules and shared helpers](#ground-rules-and-shared-helpers)
- [Send files from Explorer](#send-files-from-explorer)
- [A hot folder](#a-hot-folder)
- [Notebooks and highlights to Markdown](#notebooks-and-highlights-to-markdown)
- [Nightly backup with Task Scheduler](#nightly-backup-with-task-scheduler)
- [Reacting to changes](#reacting-to-changes)
- [Library reports](#library-reports)
- [Profiles and a second account](#profiles-and-a-second-account)
- [Windows PowerShell 5.1](#windows-powershell-51)

## Install and completion

Download the Windows ZIP for your CPU from [releases](https://github.com/altessa-s/snctl/releases).
Choose `snctl_<version>_windows_amd64.zip` or its arm64 counterpart. Unpack
the entire full build into one directory on PATH. Keep `pdfium.dll` and the
other bundled runtime DLLs beside `snctl.exe`. The `snctl-lite` ZIP includes
notebook reading and omits rendering. Both commands are named `snctl.exe`.

The release workflow also publishes both manifests to Scoop. Run
`scoop bucket add altessa-s https://github.com/altessa-s/scoop-bucket`, then
`scoop install snctl` or `scoop install snctl-lite`. Choose one shim.
To load completion now, run the following script. Add its completion line to
your PowerShell profile (`$PROFILE`) to load it in new sessions.

```powershell
#Requires -Version 7.2
snctl completion powershell | Out-String | Invoke-Expression
```

## What is different on Windows

Use Windows paths for local files and forward slashes for tablet paths such
as `DOCUMENT/Document` and `NOTE/Note`. A cloud path can start with a slash.
An existing local notebook path takes precedence when reading a notebook.
The configuration lives in `%AppData%\snctl`, the cache in
`%LocalAppData%\snctl`. Windows uses the file secret store. Run scheduled
jobs under the user who logged in and protect that user's profile directory.
Check native capabilities with `snctl version --full`. Plain output escapes a
backslash as `\\`, so a Windows path in a plain answer reads
`C:\\Users\\you\\file.pdf`; take local paths from JSON, which decodes them.

## Ground rules and shared helpers

Use JSON for structured results. Pass each argument separately. Inspect
`$LASTEXITCODE` immediately after native commands. A caught PowerShell error
does not imply that snctl rolled back a partial upload. See the
[exit-status table](automation.md#ground-rules-for-scripts).

Save **sn-common.ps1** and edit the absolute executable path and profile
before running the other scripts. For Scoop you can use the full path to
your shim. The retry helper accepts an incremental upload and replans on
exit 4. Other statuses return immediately, including verification failure
after a write (6). Every cloud writer below accepts `-DryRun`.

```powershell
#Requires -Version 7.2
$ErrorActionPreference = 'Stop'
$SnExe = 'C:\Tools\snctl\snctl.exe'
if (!$env:SNCTL_PROFILE) { $env:SNCTL_PROFILE = 'work' }
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$OutputEncoding = [Console]::OutputEncoding

function Invoke-Sn {
    & $SnExe @args
    if ($LASTEXITCODE -ne 0) { throw "snctl exited with $LASTEXITCODE" }
}

function Send-SnTree([string]$Source, [string]$Folder, [switch]$DryRun) {
    $mode = if ($DryRun) { '--dry-run' } else { '--yes' }
    foreach ($delay in 1,2,4) {
        & $SnExe mput $Source $Folder --incremental $mode
        $code = $LASTEXITCODE
        if ($code -eq 0) { return }
        if ($code -ne 4 -or $delay -eq 4) { throw "mput exited with $code" }
        Start-Sleep -Seconds $delay
    }
}
```

Dot-source the helper, then run `Send-SnTree C:\Ready DOCUMENT/Document -DryRun`
to check a directory of PDFs, EPUBs, notebooks and marks.

## Send files from Explorer

Save **sn-send.ps1**. In Explorer open `shell:sendto` and create a shortcut
to the full path of `pwsh.exe` with arguments
`-NoProfile -File "C:\Jobs\Supernote\sn-send.ps1"`. Explorer appends the
selected files. Test from a prompt with
`.\sn-send.ps1 -DryRun 'C:\Inbox\Meeting notes.pdf'`. Existing cloud names
raise a conflict.

```powershell
#Requires -Version 7.2
param([switch]$DryRun, [string]$Folder = 'DOCUMENT/Document',
      [Parameter(Position=0,ValueFromRemainingArguments)][string[]]$Files)
. "$PSScriptRoot\sn-common.ps1"
$mode = if ($DryRun) { '--dry-run' } else { '--yes' }
foreach ($path in $Files) {
    $file = Get-Item -LiteralPath $path
    if (!$file.PSIsContainer -and $file.Extension -in '.pdf','.epub') {
        Invoke-Sn put --to $Folder $mode '--' $file.FullName
    }
}
```

## A hot folder

Save **sn-hot.ps1**. Run it periodically with Task Scheduler using the
settings below. Have the producer move finished files into the inbox after
closing them. Configure the task to skip a new instance when one is running.
Preview with `.\sn-hot.ps1 -Inbox C:\Inbox -DryRun`.

```powershell
#Requires -Version 7.2
param([string]$Inbox = "$HOME\Supernote Inbox", [string]$Folder = 'DOCUMENT/Document',
      [switch]$DryRun)
. "$PSScriptRoot\sn-common.ps1"
$Inbox = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Inbox)
$sent = Join-Path $Inbox 'Sent'
foreach ($file in (Get-ChildItem -LiteralPath $Inbox -File)) {
    if ($file.Extension -notin '.pdf','.epub') { continue }
    $target = Join-Path $sent $file.Name
    if (Test-Path -LiteralPath $target) { throw "Already archived: $target" }
    & "$PSScriptRoot\sn-send.ps1" -Files $file.FullName -Folder $Folder -DryRun:$DryRun
    if (!$DryRun) {
        [IO.Directory]::CreateDirectory($sent) | Out-Null
        [IO.File]::Move($file.FullName, $target)
    }
}
```

## Notebooks and highlights to Markdown

Run **sn-vault.ps1** with `-Folder NOTE/Note/Work -Out C:\Vault\Work` or
use a document folder for highlight notes. Each filename carries the cloud
file ID so repeated titles in different folders stay separate. A hash record
keeps unchanged exports in place. A change to either the document or its
sidecar refreshes its highlights. Use a dedicated output directory per
profile. Old exports stay in the vault when a source disappears.

```powershell
#Requires -Version 7.2
param([Parameter(Mandatory)][string]$Folder, [Parameter(Mandatory)][string]$Out)
. "$PSScriptRoot\sn-common.ps1"
$Out = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Out)
[IO.Directory]::CreateDirectory($Out) | Out-Null
$items = (Invoke-Sn tree $Folder --json | Out-String | ConvertFrom-Json)
foreach ($item in $items) {
    if ($item.folder -or $item.id -notmatch '^\d+$') { continue }
    $note = $item.name -like '*.note'
    if (!$note -and !$item.mark) { continue }
    $md = Join-Path $Out "$($item.id).md"
    $stamp = "$md.hash"
    $key = "$($item.hash)/$($item.mark.hash)"
    if ((Test-Path -LiteralPath $md) -and (Test-Path -LiteralPath $stamp) -and
        [IO.File]::ReadAllText($stamp) -eq $key) { continue }
    if ($note) {
        Invoke-Sn text $item.path --format markdown -o $md
    } else {
        $lines = @(Invoke-Sn highlights $item.path --format markdown)
        [IO.File]::WriteAllLines("$md.tmp", [string[]]$lines)
        [IO.File]::Move("$md.tmp", $md, $true)
    }
    [IO.File]::WriteAllText($stamp, $key)
}
```

## Nightly backup with Task Scheduler

Save **sn-backup.ps1**. In Task Scheduler choose **Create Basic Task**, set
a daily trigger, select **Start a program** and enter the full path to
`pwsh.exe`. Use arguments `-NoProfile -File "C:\Jobs\Supernote\sn-backup.ps1"`.
Choose your logged-in user. On a laptop, open the task's **Conditions** tab
and clear **Start the task only if the computer is on AC power**: with it
set, a task started on battery stays Queued and never runs. Run the task
once. Check Last Run Result
and the backup directory. Each run verifies local bytes and retains removed
cloud files in `removed\`. See [backups](backups.md) for restore and pruning.

```powershell
#Requires -Version 7.2
param([string]$Dest = "$HOME\Backups\Supernote", [string]$Folder = '/')
. "$PSScriptRoot\sn-common.ps1"
$env:SNCTL_CACHE_TTL = '0'
Invoke-Sn backup $Dest $Folder --format plain
```

## Reacting to changes

Save **sn-change.ps1** and **sn-watch.ps1** together. Start the latter
from a prompt or schedule it at logon. The watcher passes JSON through stdin.
The handler uses the profile set by watch and reconciles the PDF tree on a
notebook event or a truncated batch. Schedule the same incremental folder
render periodically to catch changes during disconnects. Configure Task
Scheduler to restart the watcher on failure.

```powershell
#Requires -Version 7.2
. "$PSScriptRoot\sn-common.ps1"
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
$batch = [Console]::In.ReadToEnd() | ConvertFrom-Json
if ($batch.dropped -gt 0 -or ($batch.events.path -like '*.note')) {
    Invoke-Sn render NOTE/Note -o "$HOME\Supernote PDFs" --incremental
}
```

```powershell
#Requires -Version 7.2
. "$PSScriptRoot\sn-common.ps1"
$pwsh = (Get-Command pwsh.exe).Source
& $SnExe watch --only file --under /NOTE/Note --exec $pwsh '--' -NoProfile -File "$PSScriptRoot\sn-change.ps1"
exit $LASTEXITCODE
```

## Library reports

Save **sn-report.ps1**. Run it with `-Folder DOCUMENT/Document -Days 90`
to show folder sizes, duplicate files, stale documents and account usage.

```powershell
#Requires -Version 7.2
param([string]$Folder = '/', [int]$Days = 90)
. "$PSScriptRoot\sn-common.ps1"
Invoke-Sn du $Folder -d 2 --format table
Invoke-Sn dupes $Folder --format table
$cutoff = [DateTimeOffset]::Now.AddDays(-$Days)
(Invoke-Sn tree $Folder --json | Out-String | ConvertFrom-Json) |
    Where-Object { !$_.folder -and $_.modified -and [DateTimeOffset]$_.modified -lt $cutoff } |
    Select-Object path, modified
Invoke-Sn account --json | Out-String | ConvertFrom-Json | Select-Object used,total
```

## Profiles and a second account

Log in to the source and destination profiles interactively. Run
`.\sn-copy.ps1 work NOTE/Note/Work personal NOTE/Note/Imported C:\Stage -DryRun`,
then repeat without `-DryRun` to restore. The dedicated staging directory
retains the source backup, including raw marks. Identical bytes are skipped.

```powershell
#Requires -Version 7.2
param([Parameter(Mandatory)][string]$From, [Parameter(Mandatory)][string]$Source,
      [Parameter(Mandatory)][string]$To, [Parameter(Mandatory)][string]$Destination,
      [Parameter(Mandatory)][string]$Stage, [switch]$DryRun)
. "$PSScriptRoot\sn-common.ps1"
$mode = if ($DryRun) { '--dry-run' } else { '--yes' }
Invoke-Sn -P $From backup $Stage $Source
Invoke-Sn -P $To backup restore $Stage $Destination $mode
```

## Windows PowerShell 5.1

Launch the jobs above with `pwsh.exe`. Windows PowerShell 5.1 starts as
`powershell.exe`, uses different native argument handling and commonly writes
UTF-16LE with `>` and `Out-File`. For manual exports prefer snctl's
`text -o file.md` and `render -o file.pdf`. To transfer original bytes use
`get -o file`. Piping a binary download through the console can corrupt it.
