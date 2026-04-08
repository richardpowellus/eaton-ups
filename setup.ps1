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

Write-Host ""
Write-Host "  Eaton UPS — Driver Setup" -ForegroundColor Cyan
Write-Host "  ========================" -ForegroundColor Cyan
Write-Host ""

try {
    # Step 1: Find latest libusb-win32 release from GitHub
    Write-Host "  [1/4] Finding latest libusb-win32 release..." -ForegroundColor Yellow
    $apiUrl = "https://api.github.com/repos/mcuee/libusb-win32/releases/latest"
    $release = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing
    $asset = $release.assets | Where-Object { $_.name -match "^libusb-win32-bin-.*\.zip$" -and $_.name -notmatch "debug" } | Select-Object -First 1
    if (-not $asset) {
        throw "Could not find libusb-win32 binary zip in latest release ($($release.tag_name))"
    }
    Write-Host "        Latest: $($release.tag_name) — $($asset.name)" -ForegroundColor Green

    # Step 2: Download
    Write-Host "  [2/4] Downloading..." -ForegroundColor Yellow
    New-Item -ItemType Directory -Force -Path $tempDir | Out-Null
    $zipPath = Join-Path $tempDir $asset.name
    Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath -UseBasicParsing
    Write-Host "        Downloaded." -ForegroundColor Green

    # Step 3: Extract and copy DLL
    Write-Host "  [3/4] Extracting..." -ForegroundColor Yellow
    Expand-Archive -Path $zipPath -DestinationPath $tempDir -Force
    # Find the bin\amd64 directory inside the extracted folder
    $binDir = Get-ChildItem $tempDir -Directory | Where-Object { Test-Path (Join-Path $_.FullName "bin\amd64\libusb0.dll") } | Select-Object -First 1
    if (-not $binDir) { throw "Could not find bin\amd64 in extracted archive" }
    $binDir = Join-Path $binDir.FullName "bin\amd64"
    Copy-Item (Join-Path $binDir "libusb0.dll") (Join-Path $scriptDir "libusb0.dll") -Force
    Write-Host "        libusb0.dll installed." -ForegroundColor Green

    # Step 4: Install driver and device filter
    Write-Host "  [4/4] Installing USB driver..." -ForegroundColor Yellow
    $sysDir = "$env:SystemRoot\System32\drivers"
    Copy-Item (Join-Path $binDir "libusb0.sys") (Join-Path $sysDir "libusb0.sys") -Force
    Write-Host "        Copied libusb0.sys to $sysDir" -ForegroundColor Green

    # Remove stale service if left over from a previous install
    $svc = Get-Service libusb0 -ErrorAction SilentlyContinue
    if ($svc) {
        sc.exe stop libusb0 2>&1 | Out-Null
        sc.exe delete libusb0 2>&1 | Out-Null
        Start-Sleep 2
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
