# omega desktop sandbox smoke.
#
# bench\windows-sandbox.wsb maps the host's bench folder to C:\bench and runs this
# script at logon. It installs the packaged portable zip the way a user does, proves
# the instance headless, records the WebView2 registration, then launches the Start
# Menu shortcut and proves the user SAW something: the omega window, or the native
# fallback box that names the missing runtime. It uninstalls and writes every receipt
# into the mapped folder. Closing the sandbox discards the machine, so the mapped
# folder is the only thing the host keeps.
#
# The sandbox is the machine where the window path is known to die for want of a
# WebView2 runtime, so the windowed launch is proven by its fallback here and by the
# real window on a real machine. A forced PASS (a runtime install, a browser
# substitute) would hide exactly the failure this bench exists to catch.
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
$checks = @('install shape', 'headless health', 'webview2 probe', 'fallback dialog', 'uninstall', 'receipts written')
$receipts = @('log.txt', 'inventory.txt', 'health-headless.txt', 'webview2.txt', 'desktop.png', 'verdict.txt')
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
    foreach ($name in $receipts) {
        [void]$lines.Add("  " + (Join-Path $out $name))
    }
    [void]$lines.Add('')
    [void]$lines.Add('paths written:')
    [void]$lines.Add("  receipts only: $out")
    [void]$lines.Add("  sandbox-local state: " + (Join-Path $env:LOCALAPPDATA 'Programs\omega') + ", $env:TEMP\omega-portable")
    [void]$lines.Add('  host: nothing outside the mapped folder - the sandbox discards the rest')
    Set-Content -Path (Join-Path $out 'verdict.txt') -Value $lines -Encoding UTF8
}

# The runtime registration, read the way the WebView2 loader reads it: an EBWebView
# folder under the Evergreen ClientState key. The loader opens the 32-bit registry view
# (WOW6432Node on a 64-bit host), so both HKLM views are recorded beside HKCU - the
# receipt names the cause instead of guessing it. An empty value is "no runtime here":
# Edge registers the channel key on many machines and the loader still finds nothing.
function Write-WebView2Receipt([string]$path) {
    $uuid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    $lines = New-Object System.Collections.ArrayList
    [void]$lines.Add('WebView2 runtime registration (EdgeUpdate ClientState, EBWebView):')
    $versions = New-Object System.Collections.ArrayList
    $missingBinary = New-Object System.Collections.ArrayList
    $views = New-Object System.Collections.ArrayList
    if ([Environment]::Is64BitProcess) {
        [void]$views.Add([pscustomobject]@{ Hive = 'HKLM'; View = '32-bit view (the loader view)'; Path = "HKLM:\Software\WOW6432Node\Microsoft\EdgeUpdate\ClientState\$uuid" })
        [void]$views.Add([pscustomobject]@{ Hive = 'HKLM'; View = '64-bit view'; Path = "HKLM:\Software\Microsoft\EdgeUpdate\ClientState\$uuid" })
    } else {
        [void]$views.Add([pscustomobject]@{ Hive = 'HKLM'; View = '32-bit view (the loader view)'; Path = "HKLM:\Software\Microsoft\EdgeUpdate\ClientState\$uuid" })
    }
    [void]$views.Add([pscustomobject]@{ Hive = 'HKCU'; View = 'current user'; Path = "HKCU:\Software\Microsoft\EdgeUpdate\ClientState\$uuid" })
    foreach ($view in $views) {
        $label = "$($view.Hive) $($view.View)"
        if (-not (Test-Path $view.Path)) {
            [void]$lines.Add("  ${label}: key absent - $($view.Path)")
            continue
        }
        $value = (Get-ItemProperty -Path $view.Path -Name EBWebView -ErrorAction SilentlyContinue).EBWebView
        if ([string]::IsNullOrEmpty($value)) {
            [void]$lines.Add("  ${label}: key present, EBWebView empty - $($view.Path)")
        } else {
            [void]$lines.Add("  ${label}: EBWebView = $value")
            [void]$versions.Add($value)
            # The loader needs the binary at the registered path, not just the key: a
            # registry value whose folder is gone reads as "installed" to a key-only
            # probe and fails in the loader.
            $binary = Join-Path $value 'msedgewebview2.exe'
            if (Test-Path $binary) {
                [void]$lines.Add("    binary present: $binary")
            } else {
                [void]$lines.Add("    binary MISSING: $binary - a loader pointed at this registration cannot start")
                [void]$missingBinary.Add($value)
            }
        }
    }
    [void]$lines.Add('')
    [void]$lines.Add('runtime directory listing:')
    $appDir = "${env:ProgramFiles(x86)}\Microsoft\EdgeWebView\Application"
    $dirFound = @()
    if (Test-Path $appDir) {
        [void]$lines.Add("  $appDir")
        foreach ($entry in Get-ChildItem -Path $appDir -ErrorAction SilentlyContinue) {
            [void]$lines.Add("    $($entry.Name)")
            if ($entry.PSIsContainer) { $dirFound += $entry.Name }
        }
    } else {
        [void]$lines.Add("  absent: $appDir")
    }
    Set-Content -Path $path -Value $lines -Encoding UTF8

    if ($versions.Count -gt 0) {
        $summary = "runtime registered: " + (($versions | Select-Object -Unique) -join ', ')
        if ($missingBinary.Count -gt 0) {
            $summary += "; msedgewebview2.exe missing at " + (($missingBinary | Select-Object -Unique) -join ', ')
        }
        return $summary
    }
    if ($dirFound.Count -gt 0) { return "no ClientState value, runtime directory holds: " + ($dirFound -join ', ') }
    return 'no runtime registered in HKLM/HKCU and no Application directory - a fallback box is the expected desktop launch'
}

# Window discovery for the desktop proof. A Wails window is an ordinary top-level
# window titled 'omega'; the fallback is a MessageBox, whose class is #32770. Both
# carry the 'omega' caption, so the class is what tells them apart.
function Initialize-Win32 {
    if (-not ('Bench.Win32' -as [type])) {
        Add-Type -Namespace Bench -Name Win32 -MemberDefinition @'
[DllImport("user32.dll", CharSet = CharSet.Unicode)]
public static extern IntPtr FindWindow(string lpClassName, string lpWindowName);
[DllImport("user32.dll", CharSet = CharSet.Unicode)]
public static extern int GetClassName(IntPtr hWnd, System.Text.StringBuilder lpClassName, int nMaxCount);
[DllImport("user32.dll")]
public static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint lpdwProcessId);
[DllImport("user32.dll")]
public static extern bool SetWindowPos(IntPtr hWnd, IntPtr hWndInsertAfter, int X, int Y, int cx, int cy, uint uFlags);
[DllImport("user32.dll")]
public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
'@
    }
}

function Get-WindowClassName([IntPtr]$handle) {
    $text = New-Object System.Text.StringBuilder 256
    [void][Bench.Win32]::GetClassName($handle, $text, $text.Capacity)
    return $text.ToString()
}

# A window opened by a non-foreground process lands behind whatever holds the desktop,
# and Windows refuses SetForegroundWindow to a background caller: topmost does not need
# that right, so the screenshot shows the window.
function Set-Topmost([IntPtr]$handle, [bool]$on) {
    $insertAfter = if ($on) { [IntPtr](-1) } else { [IntPtr](-2) }
    [void][Bench.Win32]::ShowWindow($handle, 9)
    [void][Bench.Win32]::SetWindowPos($handle, $insertAfter, 0, 0, 0, 0, 0x0001 -bor 0x0002 -bor 0x0040)
}

function Stop-Omega {
    Get-Process -Name omega -ErrorAction SilentlyContinue | ForEach-Object { $_.CloseMainWindow() | Out-Null }
    Start-Sleep -Seconds 2
    Get-Process -Name omega -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
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

    # Recorded before the launch, not after: the probe is the context the launch's
    # outcome is read against. PASS is "recorded", never "installed" - the sandbox is
    # expected to answer "absent".
    $phase = 'webview2 probe'
    try {
        $summary = Write-WebView2Receipt (Join-Path $out 'webview2.txt')
        Add-Result 'webview2 probe' $true $summary
    } catch {
        Add-Result 'webview2 probe' $false "could not write webview2.txt: $($_.Exception.Message)"
    }

    # The user path, not the exe directly: launching the shortcut proves the shortcut.
    # The desktop proof is "the user saw something": the window, or the fallback box.
    $phase = 'fallback dialog'
    $logPath = Join-Path $dest 'omega.log'
    $logBefore = 0
    if (Test-Path $logPath) {
        $logBefore = @(Get-Content -Path $logPath -ErrorAction SilentlyContinue).Count
    }

    Start-Process -FilePath $lnk

    Initialize-Win32
    $deadline = (Get-Date).AddSeconds($firstBootBudget)
    $dialogHandle = [IntPtr]::Zero
    $window = $null
    while ((Get-Date) -lt $deadline) {
        $dialogHandle = [Bench.Win32]::FindWindow('#32770', 'omega')
        if ($dialogHandle -ne [IntPtr]::Zero) { break }
        foreach ($p in @(Get-Process -Name omega -ErrorAction SilentlyContinue)) {
            if ($p.MainWindowHandle -eq [IntPtr]::Zero) { continue }
            if ($p.MainWindowTitle -ne 'omega') { continue }
            if ((Get-WindowClassName $p.MainWindowHandle) -eq '#32770') { continue }
            $window = $p
            break
        }
        if ($window) { break }
        Start-Sleep -Seconds 2
    }

    # A cold first boot of the packaged exe measured a minute on a warm machine, so this
    # wait bounds a HANG; it is not a boot-time assertion.
    $shape = 'none'
    $targetHandle = [IntPtr]::Zero
    $pidText = ''
    if ($dialogHandle -ne [IntPtr]::Zero) {
        $shape = 'dialog'
        $targetHandle = $dialogHandle
        $boxPid = 0
        [void][Bench.Win32]::GetWindowThreadProcessId($dialogHandle, [ref]$boxPid)
        $pidText = "pid $boxPid"
        Write-Host "saw: fallback dialog box (#32770) titled 'omega' ($pidText)"
    } elseif ($window) {
        $shape = 'window'
        $targetHandle = $window.MainWindowHandle
        $pidText = "pid $($window.Id)"
        Write-Host "saw: window titled '$($window.MainWindowTitle)' ($pidText)"
    } else {
        Write-Host "saw: neither a window nor a fallback box titled 'omega' within $firstBootBudget s"
    }

    $raised = $false
    if ($targetHandle -ne [IntPtr]::Zero) {
        try {
            Set-Topmost $targetHandle $true
            $raised = $true
        } catch {
            Write-Host "could not raise the window: $($_.Exception.Message)"
        }
        # Let the frame paint: the screenshot is the evidence a reviewer reads.
        Start-Sleep -Seconds 3
    }

    # Captured on success and failure alike: a missing desktop photographed is a
    # diagnosis, a missing desktop unphotographed is a guess.
    $desktopPng = Join-Path $out 'desktop.png'
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
    if ($raised) {
        try { Set-Topmost $targetHandle $false }
        catch { Write-Host "could not unset topmost: $($_.Exception.Message)" }
    }

    # Whatever the shape: a running exe holds its own file open and blocks the
    # uninstall, and the log is read after the process that writes it has gone.
    Stop-Omega

    $newLines = @()
    if (Test-Path $logPath) {
        $all = @(Get-Content -Path $logPath -ErrorAction SilentlyContinue)
        if ($all.Count -gt $logBefore) { $newLines = @($all[$logBefore..($all.Count - 1)]) }
    }
    if ($newLines.Count -gt 0) {
        Write-Host 'new omega.log lines:'
        foreach ($line in $newLines) { Write-Host "  $line" }
    }
    $webview2Line = $newLines | Where-Object { $_ -match 'WebView2' } | Select-Object -First 1

    if ($shotError) {
        Add-Result 'fallback dialog' $false "$shape found, but the screenshot failed: $shotError"
    } elseif ($shape -eq 'dialog') {
        if ($webview2Line) {
            Add-Result 'fallback dialog' $true "fallback box (#32770, $pidText) photographed to desktop.png; omega.log gained: $($webview2Line.Trim())"
        } else {
            Add-Result 'fallback dialog' $false 'the box was photographed but omega.log gained no line naming WebView2 since the launch'
        }
    } elseif ($shape -eq 'window') {
        Add-Result 'fallback dialog' $true "window titled '$($window.MainWindowTitle)' ($pidText) photographed to desktop.png"
    } else {
        Add-Result 'fallback dialog' $false "no window and no fallback box titled 'omega' within $firstBootBudget s; desktop.png kept as the evidence"
    }

    # Ungated: the wipe must hold whoever showed up on the desktop.
    $phase = 'uninstall'
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
} catch {
    Add-Result $phase $false $_.Exception.Message
} finally {
    Stop-Omega
}

# Last, because every file above must exist before it can be counted.
$phase = 'receipts written'
$missingReceipts = @(@('log.txt', 'inventory.txt', 'health-headless.txt', 'webview2.txt', 'desktop.png') | Where-Object {
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
