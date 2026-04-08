<#
.SYNOPSIS
    Sets up the libusb0 driver and DLL for Eaton UPS devices.
.DESCRIPTION
    Downloads libusb-win32 from GitHub, extracts the 64-bit DLL and driver,
    and installs libusb0 as a device filter for the Eaton UPS.
    No Eaton software or driver signing required.
    Must be run as Administrator.
.EXAMPLE
    .\setup.ps1
#>

# Always pause before closing so the user can read output
trap { Write-Host "`n  ERROR: $_" -ForegroundColor Red }
$null = Register-EngineEvent PowerShell.Exiting -Action { cmd /c pause }

# Check admin
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host ""
    Write-Host "  This script must be run as Administrator." -ForegroundColor Red
    Write-Host "  Right-click PowerShell → 'Run as administrator', then try again."
    cmd /c pause
    exit 1
}

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$tempDir = Join-Path $env:TEMP "eaton-ups-setup"
$release = "release_1.4.0.2"
$zipName = "libusb-win32-bin-1.4.0.2"
$zipUrl = "https://github.com/mcuee/libusb-win32/releases/download/$release/$zipName.zip"

Write-Host ""
Write-Host "  Eaton UPS — Driver Setup" -ForegroundColor Cyan
Write-Host "  ========================" -ForegroundColor Cyan
Write-Host ""

try {
    # Step 1: Download libusb-win32
    Write-Host "  [1/4] Downloading libusb-win32..." -ForegroundColor Yellow
    New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
    $zipPath = Join-Path $tempDir "$zipName.zip"
    if (-not (Test-Path $zipPath)) {
        Invoke-WebRequest -Uri $zipUrl -OutFile $zipPath -UseBasicParsing
    }
    Write-Host "        Downloaded." -ForegroundColor Green

    # Step 2: Extract
    Write-Host "  [2/4] Extracting..." -ForegroundColor Yellow
    $extractDir = Join-Path $tempDir $zipName
    if (Test-Path $extractDir) { Remove-Item $extractDir -Recurse -Force }
    Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force
    $binDir = Join-Path $extractDir "bin\amd64"
    Write-Host "        Extracted." -ForegroundColor Green

    # Step 3: Copy DLL next to this script
    Write-Host "  [3/4] Copying libusb0.dll..." -ForegroundColor Yellow
    Copy-Item (Join-Path $binDir "libusb0.dll") (Join-Path $scriptDir "libusb0.dll") -Force
    Write-Host "        Done." -ForegroundColor Green

    # Step 4: Copy driver sys and install filter
    Write-Host "  [4/4] Installing USB driver..." -ForegroundColor Yellow
    $sysDir = "$env:SystemRoot\System32\drivers"
    if (-not (Test-Path (Join-Path $sysDir "libusb0.sys"))) {
        Copy-Item (Join-Path $binDir "libusb0.sys") (Join-Path $sysDir "libusb0.sys") -Force
        Write-Host "        Copied libusb0.sys to $sysDir" -ForegroundColor Green
    }

    # Install libusb0 as a device filter (no signed INF needed)
    $filterExe = Join-Path $binDir "install-filter.exe"
    Write-Host "        Installing device filter for Eaton UPS..." -ForegroundColor Yellow
    $result = & $filterExe install "--device=USB\VID_0463&PID_FFFF" 2>&1
    foreach ($line in $result) {
        $s = "$line".Trim()
        if ($s) { Write-Host "        $s" }
    }

    # Verify
    Start-Sleep 2
    $dev = Get-PnpDevice -ErrorAction SilentlyContinue |
        Where-Object { $_.InstanceId -match "USB\\VID_0463&PID_FFFF" -and $_.Status -eq "OK" }
    if ($dev) {
        Write-Host ""
        Write-Host "        Device: $($dev.FriendlyName) [$($dev.Service)]" -ForegroundColor Green
    } else {
        Write-Host ""
        Write-Host "        Device not detected yet. Try unplugging and re-plugging USB." -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "  Setup complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Run:  .\eaton-ups.exe status"
    Write-Host ""

} catch {
    Write-Host ""
    Write-Host "  Setup failed: $_" -ForegroundColor Red
    Write-Host ""
} finally {
    # Clean up temp files
    Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}

cmd /c pause
