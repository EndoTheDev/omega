#!/usr/bin/env pwsh
# Packages the Windows desktop asset: dist/omega-<version>-windows-desktop.zip
#
# Shape: the portable tree (omega.exe, omega.cmd, the example configs) plus install.ps1 /
# uninstall.ps1, which copy into %LOCALAPPDATA%\Programs\omega, create the Start
# Menu shortcut and write the HKCU uninstall entry. Wails v3's own packaging
# targets (nsis/msix) plan from a wails.yaml project manifest this repo does not
# carry, so the installer is the two scripts, not an external toolchain.
#
# Usage: pwsh scripts/desktop-package.ps1 -Version v0.19.0 -Exe dist/omega.exe
[CmdletBinding()]
param(
	[Parameter(Mandatory = $true)][string]$Version,
	[Parameter(Mandatory = $true)][string]$Exe,
	[string]$OutDir = 'dist',
	[string]$ExamplesDir = 'cmd/omega/examples',
	[string]$Shim = 'cmd/omega/omega.cmd'
)

$ErrorActionPreference = 'Stop'

$name = "omega-$Version-windows-desktop"
$stage = Join-Path $OutDir $name
$zip = Join-Path $OutDir "$name.zip"

if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Force -Path $stage | Out-Null

Copy-Item -Path $Exe -Destination (Join-Path $stage 'omega.exe') -Force
Copy-Item -Path $Shim -Destination $stage -Force
Copy-Item -Path (Join-Path $ExamplesDir 'config.yaml.example') -Destination $stage -Force
Copy-Item -Path (Join-Path $ExamplesDir 'mcp.yaml.example') -Destination $stage -Force

$displayVersion = $Version.TrimStart('v')

$install = @'
# omega desktop installer: run it from the extracted folder.
$ErrorActionPreference = 'Stop'

$src = $PSScriptRoot
$dest = if ($env:OMEGA_INSTALL_DIR) { $env:OMEGA_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\omega' }

New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item -Path (Join-Path $src 'omega.exe') -Destination $dest -Force
Copy-Item -Path (Join-Path $src 'omega.cmd') -Destination $dest -Force
Copy-Item -Path (Join-Path $src 'uninstall.ps1') -Destination $dest -Force
Get-ChildItem -Path $src -Filter '*.example' -File | Copy-Item -Destination $dest -Force

$exe = Join-Path $dest 'omega.exe'
$lnk = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\omega.lnk'
$sc = (New-Object -ComObject WScript.Shell).CreateShortcut($lnk)
$sc.TargetPath = $exe
$sc.Arguments = 'desktop'
$sc.WorkingDirectory = $dest
$sc.Description = 'omega desktop'
$sc.Save()

$key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\omega'
New-Item -Path $key -Force | Out-Null
Set-ItemProperty -Path $key -Name DisplayName -Value 'omega desktop'
Set-ItemProperty -Path $key -Name DisplayVersion -Value '@@VERSION@@'
Set-ItemProperty -Path $key -Name Publisher -Value 'EndoTheDev'
Set-ItemProperty -Path $key -Name InstallLocation -Value $dest
Set-ItemProperty -Path $key -Name DisplayIcon -Value $exe
Set-ItemProperty -Path $key -Name UninstallString -Value ('powershell.exe -NoProfile -ExecutionPolicy Bypass -File "' + (Join-Path $dest 'uninstall.ps1') + '"')
Set-ItemProperty -Path $key -Name NoModify -Value 1 -Type DWord
Set-ItemProperty -Path $key -Name NoRepair -Value 1 -Type DWord

Write-Host "omega desktop @@VERSION@@ installed to $dest"
Write-Host "Start Menu: $lnk"
Write-Host "First run: 'omega setup', then launch omega from the Start Menu."
'@ -replace '@@VERSION@@', $displayVersion

$uninstall = @'
# omega desktop uninstaller: removes the shortcut, the uninstall entry and the tree.
$ErrorActionPreference = 'Stop'

$dest = if ($env:OMEGA_INSTALL_DIR) { $env:OMEGA_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\omega' }
$lnk = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\omega.lnk'
$key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\omega'

Remove-Item -Path $lnk -Force -ErrorAction SilentlyContinue
if (Test-Path $key) { Remove-Item -Path $key -Recurse -Force }

# Run from the installed tree, this script is the process's own file: move to a
# temp copy and let the copy delete the tree, then itself.
$self = $MyInvocation.MyCommand.Path
$runningFromTree = $self -and ($self -like (Join-Path $dest '*'))
if ($runningFromTree) {
	$copy = Join-Path $env:TEMP 'omega-desktop-uninstall.ps1'
	Copy-Item -Path $self -Destination $copy -Force
	Write-Host "omega desktop uninstalled ($dest)"
	& powershell.exe -NoProfile -ExecutionPolicy Bypass -File $copy
	exit $LASTEXITCODE
}

if (Test-Path $dest) { Remove-Item -Path $dest -Recurse -Force }
$tempCopy = Join-Path $env:TEMP 'omega-desktop-uninstall.ps1'
if ($PSCommandPath -and ($PSCommandPath -like $tempCopy)) { Remove-Item -Path $PSCommandPath -Force }
Write-Host "omega desktop uninstalled"
'@

Set-Content -Path (Join-Path $stage 'install.ps1') -Value $install -Encoding utf8
Set-Content -Path (Join-Path $stage 'uninstall.ps1') -Value $uninstall -Encoding utf8

if (Test-Path $zip) { Remove-Item -Force $zip }
Compress-Archive -Path $stage -DestinationPath $zip
Write-Host "packaged $zip"
