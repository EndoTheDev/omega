# omega desktop sandbox smoke.
#
# bench\windows-sandbox.wsb maps the host's bench folder to C:\bench and runs this
# script at logon. It installs the packaged portable zip the way a user does, boots
# the desktop, proves the instance and the window, uninstalls, and writes every
# receipt into the mapped folder. Closing the sandbox discards the machine, so the
# mapped folder is the only thing the host keeps.
#
# Windows PowerShell 5.1 with OS modules only: the sandbox has no package feed.

$ErrorActionPreference = 'Stop'

$root = 'C:\bench'
$firstBootBudget = 180
$run = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')
$out = Join-Path $root "out\$run"
New-Item -ItemType Directory -Force -Path $out | Out-Null
Start-Transcript -Path (Join-Path $out 'log.txt') -Force | Out-Null

# Every check gets one verdict line, including the ones a fatal failure never
# reached: a silent skip would read as a pass.
$checks = @('install shape', 'headless health', 'instance health', 'window screenshot', 'uninstall', 'receipts written')
$results = New-Object System.Collections.ArrayList
$phase = 'install shape'

function Add-Result([string]$name, [bool]$ok, [string]$detail) {
    [void]$results.Add([pscustomobject]@{ Name = $name; Ok = $ok; Detail = $detail })
    if ($ok) { Write-Host "PASS $name" } else { Write-Host "FAIL $name - $detail" }
}

# Windows PowerShell 5.1 turns a native command's stderr into a terminating error
# while ErrorActionPreference is Stop, so every external call goes through here:
# output and exit code come back as data, never as an exception.
function Invoke-Native([string]$file, [string[]]$arguments) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $text = (& $file @arguments 2>&1 | ForEach-Object { "$_" } | Out-String)
        $exit = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
    return [pscustomobject]@{ Output = $text; ExitCode = $exit }
}

function Write-Verdict {
    $lines = New-Object System.Collections.ArrayList
    [void]$lines.Add("omega desktop sandbox smoke - run $run")
    [void]$lines.Add('')
    foreach ($check in $checks) {
        $r = $results | Where-Object { $_.Name -eq $check } | Select-Object -First 1
        if ($r) {
            $state = if ($r.Ok) { 'PASS' } else { 'FAIL' }
            [void]$lines.Add(("{0} {1} - {2}" -f $state, $check, $r.Detail))
        } else {
            [void]$lines.Add(("FAIL {0} - not reached" -f $check))
        }
    }
    [void]$lines.Add('')
    [void]$lines.Add('receipts:')
    foreach ($name in @('log.txt', 'inventory.txt', 'health-headless.txt', 'health-instance.txt', 'desktop.png', 'verdict.txt')) {
        [void]$lines.Add("  " + (Join-Path $out $name))
    }
    [void]$lines.Add('')
    [void]$lines.Add('paths written:')
    [void]$lines.Add("  receipts only: $out")
    [void]$lines.Add("  sandbox-local state: " + (Join-Path $env:LOCALAPPDATA 'Programs\omega') + ", $env:TEMP\omega-portable")
    [void]$lines.Add('  host: nothing outside the mapped folder - the sandbox discards the rest')
    Set-Content -Path (Join-Path $out 'verdict.txt') -Value $lines -Encoding UTF8
}

try {
    # The host stages exactly one packaged asset in the mapped folder; anything else
    # means the run would test a tree nobody chose.
    $zips = @(Get-ChildItem -Path $root -Filter 'omega-*-windows-desktop.zip' -File)
    if ($zips.Count -ne 1) {
        $found = if ($zips.Count -eq 0) { 'none' } else { ($zips | ForEach-Object { $_.Name }) -join ', ' }
        throw "expected exactly one omega-*-windows-desktop.zip directly in $root, found $($zips.Count): $found"
    }
    Write-Host "asset: $($zips[0].FullName)"

    $expand = Join-Path $env:TEMP 'omega-portable'
    if (Test-Path $expand) { Remove-Item -Recurse -Force $expand }
    Expand-Archive -Path $zips[0].FullName -DestinationPath $expand -Force
    $trees = @(Get-ChildItem -Path $expand -Directory -Filter 'omega-*')
    if ($trees.Count -ne 1) { throw "expected one omega-* directory under $expand, found $($trees.Count)" }
    $installer = Join-Path $trees[0].FullName 'install.ps1'
    if (-not (Test-Path $installer)) { throw "install.ps1 missing from $($zips[0].Name)" }

    $install = Invoke-Native 'powershell.exe' @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $installer)
    Write-Host $install.Output
    if ($install.ExitCode -ne 0) { throw "install.ps1 exited $($install.ExitCode)" }

    # The install shape the release workflow gates on: exe, shim, Start Menu shortcut
    # and an HKCU uninstall entry carrying an UninstallString.
    $dest = Join-Path $env:LOCALAPPDATA 'Programs\omega'
    $exe = Join-Path $dest 'omega.exe'
    $shim = Join-Path $dest 'omega.cmd'
    $lnk = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\omega.lnk'
    $key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\omega'

    $missing = @()
    foreach ($p in @($exe, $shim, $lnk)) { if (-not (Test-Path $p)) { $missing += $p } }
    $props = $null
    if (Test-Path $key) {
        $props = Get-ItemProperty -Path $key
        if (-not $props.UninstallString) { $missing += "$key UninstallString" }
    } else {
        $missing += $key
    }

    $inventory = New-Object System.Collections.ArrayList
    [void]$inventory.Add("install dir: $dest")
    [void]$inventory.Add('')
    [void]$inventory.Add('tree (bytes, path):')
    if (Test-Path $dest) {
        foreach ($f in Get-ChildItem -Path $dest -Recurse -File | Sort-Object FullName) {
            [void]$inventory.Add(('{0,10}  {1}' -f $f.Length, $f.FullName))
        }
    }
    [void]$inventory.Add('')
    [void]$inventory.Add("shortcut: $lnk")
    if (Test-Path $lnk) {
        $sc = (New-Object -ComObject WScript.Shell).CreateShortcut($lnk)
        [void]$inventory.Add("  target: $($sc.TargetPath)")
        [void]$inventory.Add("  arguments: $($sc.Arguments)")
        [void]$inventory.Add("  working directory: $($sc.WorkingDirectory)")
    }
    [void]$inventory.Add('')
    [void]$inventory.Add("uninstall key: $key")
    if ($props) {
        foreach ($p in $props.PSObject.Properties) {
            if ($p.Name -like 'PS*') { continue }
            [void]$inventory.Add("  $($p.Name) = $($p.Value)")
        }
    }
    Set-Content -Path (Join-Path $out 'inventory.txt') -Value $inventory -Encoding UTF8

    if ($missing.Count -gt 0) {
        Add-Result 'install shape' $false ("missing: " + ($missing -join ', '))
    } else {
        Add-Result 'install shape' $true "installed under $dest; shortcut and uninstall entry present"
    }

    # The installed exe directory IS the home (OMEGA_HOME, else the exe directory), so a
    # fresh install needs no OMEGA_HOME: the config sits beside the exe. mock answers
    # deterministically with no network, port 8099 is the default loopback the window
    # loads, and approvals off keeps a flagged command from parking on a verdict.
    $cfgDir = Join-Path $dest 'config'
    New-Item -ItemType Directory -Force -Path $cfgDir | Out-Null
    $yaml = @'
provider:
  model_name: mock-model
  instances:
    - name: default
      type: mock
      host: ""
      api_key: ""
server:
  port: 8099
approvals:
  mode: off
'@
    Set-Content -Path (Join-Path $cfgDir 'config.yaml') -Value $yaml -Encoding ascii

    $phase = 'headless health'
    $headless = Join-Path $out 'health-headless.txt'
    $probe = Invoke-Native $exe @('desktop', '--headless-health')
    Set-Content -Path $headless -Value @("exit: $($probe.ExitCode)", '', $probe.Output) -Encoding UTF8
    if ($probe.ExitCode -eq 0 -and $probe.Output -match '"status":"ok"') {
        Add-Result 'headless health' $true 'omega desktop --headless-health exited 0 with status ok'
    } else {
        Add-Result 'headless health' $false "exit $($probe.ExitCode), body: $($probe.Output.Trim())"
    }

    # The user path, not the exe directly: launching the shortcut proves the shortcut.
    $phase = 'instance health'
    Start-Process -FilePath $lnk
    $instance = Join-Path $out 'health-instance.txt'
    $probes = New-Object System.Collections.ArrayList
    # A cold first boot of the packaged exe measured a minute on a warm machine, so this
    # wait bounds a HANG; it is not a boot-time assertion.
    $deadline = (Get-Date).AddSeconds($firstBootBudget)
    $instanceOk = $false
    while ((Get-Date) -lt $deadline) {
        $probe = Invoke-Native $exe @('health')
        [void]$probes.Add("$((Get-Date).ToUniversalTime().ToString('HH:mm:ssZ')) exit $($probe.ExitCode)")
        [void]$probes.Add($probe.Output.TrimEnd())
        if ($probe.ExitCode -eq 0 -and $probe.Output -match 'ok: http://localhost:8099') { $instanceOk = $true; break }
        Start-Sleep -Seconds 2
    }
    Set-Content -Path $instance -Value $probes -Encoding UTF8
    if ($instanceOk) {
        Add-Result 'instance health' $true 'omega health answered ok: http://localhost:8099'
    } else {
        Add-Result 'instance health' $false "no ok: http://localhost:8099 line within $firstBootBudget s (probes in health-instance.txt)"
    }

    $phase = 'window screenshot'
    $desktopPng = Join-Path $out 'desktop.png'
    $deadline = (Get-Date).AddSeconds($firstBootBudget)
    $window = $null
    while ((Get-Date) -lt $deadline) {
        $window = Get-Process -Name omega -ErrorAction SilentlyContinue |
            Where-Object { $_.MainWindowTitle -eq 'omega' } | Select-Object -First 1
        if ($window) { break }
        Start-Sleep -Seconds 2
    }
    if ($window) {
        Write-Host "window title: '$($window.MainWindowTitle)' (pid $($window.Id))"
        # A window opened by a non-foreground process lands behind whatever holds the
        # desktop, and Windows refuses SetForegroundWindow to a background caller:
        # topmost does not need that right, so the screenshot shows the window.
        $raised = $false
        try {
            Add-Type -Namespace Bench -Name Win32 -MemberDefinition @'
[DllImport("user32.dll")]
public static extern bool SetWindowPos(IntPtr hWnd, IntPtr hWndInsertAfter, int X, int Y, int cx, int cy, uint uFlags);
[DllImport("user32.dll")]
public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
'@
            [void][Bench.Win32]::ShowWindow($window.MainWindowHandle, 9)
            # SWP_NOSIZE | SWP_NOMOVE | SWP_SHOWWINDOW
            [void][Bench.Win32]::SetWindowPos($window.MainWindowHandle, [IntPtr](-1), 0, 0, 0, 0, 0x0001 -bor 0x0002 -bor 0x0040)
            $raised = $true
        } catch {
            Write-Host "could not raise the window: $($_.Exception.Message)"
        }
        # Let the SPA paint: the screenshot is the evidence a reviewer reads.
        Start-Sleep -Seconds 3
    }

    # Captured on success and failure alike: a missing window photographed is a
    # diagnosis, a missing window unphotographed is a guess.
    $shotError = $null
    try {
        Add-Type -AssemblyName System.Windows.Forms
        Add-Type -AssemblyName System.Drawing
        $bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
        $bmp = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
        try {
            $gfx = [System.Drawing.Graphics]::FromImage($bmp)
            try { $gfx.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size) }
            finally { $gfx.Dispose() }
            $bmp.Save($desktopPng, [System.Drawing.Imaging.ImageFormat]::Png)
        } finally { $bmp.Dispose() }
    } catch {
        $shotError = $_.Exception.Message
        Write-Host "screenshot failed: $shotError"
    }

    # The bench raised the window for the capture only: hand the desktop back.
    if ($window -and $raised) {
        try { [void][Bench.Win32]::SetWindowPos($window.MainWindowHandle, [IntPtr](-2), 0, 0, 0, 0, 0x0001 -bor 0x0002) }
        catch { Write-Host "could not unset topmost: $($_.Exception.Message)" }
    }

    $windowOk = $false
    if (-not $window) {
        Add-Result 'window screenshot' $false "no omega process with MainWindowTitle 'omega' within $firstBootBudget s; desktop.png kept as the evidence"
    } elseif ($shotError) {
        Add-Result 'window screenshot' $false "window '$($window.MainWindowTitle)' found, screenshot failed: $shotError"
    } else {
        Add-Result 'window screenshot' $true "window titled 'omega' (pid $($window.Id)) captured to desktop.png"
        $windowOk = $true
    }

    # A window this bench cannot see is the run's finding: report it with the log and
    # the screenshot, never paper over it with a runtime install or a browser.
    if ($windowOk) {
        $phase = 'uninstall'
        # A running exe holds its own file open, so the app stops before the tree goes.
        Get-Process -Name omega -ErrorAction SilentlyContinue | ForEach-Object { $_.CloseMainWindow() | Out-Null }
        Start-Sleep -Seconds 2
        Get-Process -Name omega -ErrorAction SilentlyContinue | Stop-Process -Force
        Start-Sleep -Seconds 1

        $uninstaller = Join-Path $dest 'uninstall.ps1'
        $uninstall = Invoke-Native 'powershell.exe' @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $uninstaller)
        Write-Host $uninstall.Output

        $left = @()
        foreach ($p in @($exe, $shim, $lnk, $key, $dest)) { if (Test-Path $p) { $left += $p } }
        if ($uninstall.ExitCode -ne 0) {
            Add-Result 'uninstall' $false "uninstall.ps1 exited $($uninstall.ExitCode)"
        } elseif ($left.Count -gt 0) {
            Add-Result 'uninstall' $false ("left behind: " + ($left -join ', '))
        } else {
            Add-Result 'uninstall' $true 'exe, shim, shortcut, uninstall entry and install dir all gone'
        }
    }
} catch {
    Add-Result $phase $false $_.Exception.Message
}

# Last, because every file above must exist before it can be counted.
$phase = 'receipts written'
$missingReceipts = @(@('log.txt', 'inventory.txt', 'health-headless.txt', 'health-instance.txt', 'desktop.png') | Where-Object {
        -not (Test-Path (Join-Path $out $_))
    })
if ($missingReceipts.Count -eq 0) {
    Add-Result 'receipts written' $true "all receipts under $out"
} else {
    Add-Result 'receipts written' $false ("missing: " + ($missingReceipts -join ', '))
}

Write-Verdict
Stop-Transcript -ErrorAction SilentlyContinue

$failed = @($results | Where-Object { -not $_.Ok })
if ($failed.Count -eq 0) { exit 0 }
exit 1
