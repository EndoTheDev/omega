# sweep-chromes.ps1 - reap orphaned headless chromiums on Windows.
#
# The no-/proc half of sweep-chromes.sh: same contract, same safety law,
# different process table. Facts come from Win32_Process instead of
# /proc, plus one rule the /proc version gets for free:
#
#   PID RECYCLING. Windows hands a dead parent's pid to the next process
#   that asks for one. Observed on this host (2026-11-11): 9 orphaned
#   agent-browser mains looked "parented" because their dead daemon's pid
#   had landed on a live, unrelated chrome.exe. A parent can only predate
#   its child, so a resolved parent whose creation stamp is LATER than the
#   child's is a stranger wearing a recycled pid - the child is an orphan.
#
# Matched profile shapes (--user-data-dir):
#   omega-journeys-chrome-profile-<port>   journey launch (run.sh)
#   dbgN-chrome-profile                    worker debug chrome
#   agent-browser-chrome-<uuid>            agent-browser auto-launch
#
# Safety:
#   - MAIN process only: --remote-debugging-port present, --type= absent.
#     Renderers and crashpad children carry the same profile string.
#   - A chrome whose parent is alive and older is NEVER touched: that is a
#     running suite or a worker's live debug chrome.
#   - Kill by pid, never by pattern text. Nothing else on the host matches.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File sweep-chromes.ps1 [-Quiet] [-DryRun]
#
#   -DryRun        print the kill list, kill nothing, always exit 0. The
#                  decision is the part worth checking; sweep_chromes_win_test.sh
#                  drives it.
#   SWEEP_PROC_JSON=<file>
#                  replay a captured process table (JSON array of
#                  {ProcessId,ParentProcessId,Name,CommandLine,CreationDate})
#                  instead of the live one. A replay is ALWAYS dry - a
#                  synthetic table must never be able to kill a live pid.
#
# Exit: 0 none killed, 1 orphans killed (same as the bash half).

param(
  [switch]$Quiet,
  [switch]$DryRun
)

$ErrorActionPreference = 'Continue'

if ($env:SWEEP_PROC_JSON) {
  # ConvertFrom-Json hands back one Object[] for a JSON array; do NOT wrap
  # it in @() - that leaves the array as a single row and every lookup
  # below sees Object[] where it wants an int.
  $table = Get-Content -Raw -LiteralPath $env:SWEEP_PROC_JSON | ConvertFrom-Json
  if ($table -isnot [array]) { $table = @($table) }
  $DryRun = $true
} else {
  $table = @(Get-CimInstance -ClassName Win32_Process -ErrorAction SilentlyContinue)
}
$byId = @{}
foreach ($p in $table) { $byId[[int]$p.ProcessId] = $p }

$shapes = @('chrome-profile', 'agent-browser-chrome-')
$mains = @()
foreach ($p in $table) {
  if ($p.Name -ne 'chrome.exe') { continue }
  $cl = [string]$p.CommandLine
  if (-not $cl) { continue }
  if ($cl -match '--type=') { continue }
  if ($cl -notlike '*--user-data-dir=*') { continue }
  if ($cl -notlike '*--remote-debugging-port*') { continue }
  $shaped = $false
  foreach ($s in $shapes) { if ($cl -like "*$s*") { $shaped = $true; break } }
  if (-not $shaped) { continue }
  $par = $byId[[int]$p.ParentProcessId]
  if ($par -and ([datetime]$par.CreationDate -le [datetime]$p.CreationDate)) { continue }
  $mains += $p
}

foreach ($p in $mains) {
  if (-not $Quiet) {
    $ud = $null
    if ($p.CommandLine -match '--user-data-dir=(?:"([^"]*)"|(\S+))') {
      if ($Matches[1]) { $ud = $Matches[1] } else { $ud = $Matches[2] }
    }
    $verb = if ($DryRun) { 'would kill' } else { 'killing' }
    Write-Output ("sweep-chromes: {0} orphan chrome pid={1} {2}" -f $verb, $p.ProcessId, $ud)
  }
  if ($DryRun) { continue }
  Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
}

# belt: a renderer that outlived its main. Same dead-or-recycled rule on the
# child's parent - a pid that got recycled must not drag a stranger's
# renderer down with it.
if (-not $DryRun -and $mains.Count -gt 0) {
  Start-Sleep -Milliseconds 1000
  $killed = @{}
  foreach ($m in $mains) { $killed[[int]$m.ProcessId] = $m }
  foreach ($p in @(Get-CimInstance -ClassName Win32_Process -ErrorAction SilentlyContinue)) {
    if ($p.Name -ne 'chrome.exe') { continue }
    $par = $killed[[int]$p.ParentProcessId]
    if (-not $par) { continue }
    if ([datetime]$p.CreationDate -lt [datetime]$par.CreationDate) { continue }
    Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
  }
}

if ($mains.Count -gt 0) {
  if ($DryRun) {
    Write-Output ("sweep-chromes: {0} orphaned chrome(s) would be reaped" -f $mains.Count)
    exit 0
  }
  Write-Output ("sweep-chromes: reaped {0} orphaned chrome(s)" -f $mains.Count)
  exit 1
}
if (-not $Quiet) { Write-Output 'sweep-chromes: no orphaned chrome processes' }
exit 0
