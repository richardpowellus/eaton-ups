<#
.SYNOPSIS
    Sets up the libusb0 driver and DLL for Eaton UPS devices.
.DESCRIPTION
    Downloads libusb-win32 from GitHub, extracts the 64-bit DLL and driver,
    and installs the USB driver for Eaton UPS devices. No Eaton software needed.
    Must be run as Administrator.
.EXAMPLE
    .\setup.ps1
#>

$ErrorActionPreference = "Stop"

# Check admin
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "This script must be run as Administrator." -ForegroundColor Red
    Write-Host "Right-click PowerShell and select 'Run as administrator', then try again."
    exit 1
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$tempDir = Join-Path $env:TEMP "eaton-ups-setup"
$release = "release_1.4.0.2"
$zipName = "libusb-win32-bin-1.4.0.2"
$zipUrl = "https://github.com/mcuee/libusb-win32/releases/download/$release/$zipName.zip"

Write-Host ""
Write-Host "  Eaton UPS — Driver Setup" -ForegroundColor Cyan
Write-Host "  ========================" -ForegroundColor Cyan
Write-Host ""

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
Write-Host "        Extracted." -ForegroundColor Green

# Step 3: Copy DLL next to this script (and the exe)
Write-Host "  [3/4] Installing libusb0.dll..." -ForegroundColor Yellow
$dllSrc = Join-Path $extractDir "bin\amd64\libusb0.dll"
$dllDst = Join-Path $scriptDir "libusb0.dll"
Copy-Item $dllSrc $dllDst -Force
Write-Host "        Copied to $dllDst" -ForegroundColor Green

# Step 4: Install the driver
Write-Host "  [4/4] Installing USB driver for Eaton UPS devices..." -ForegroundColor Yellow

# Create INF file for the driver
$infContent = @"
[Version]
Signature="`$WINDOWS NT`$"
Provider=%Manufacturer%
DriverVer=01/09/2025,1.4.0.2
Class=libusb-win32 devices
ClassGUID={EB781AAF-9C70-4523-A5DF-642A87ECA567}

[ClassInstall32]
AddReg=ClassInstall32.AddReg

[ClassInstall32.AddReg]
HKR,,,,"libusb-win32 devices"
HKR,,Icon,,"-20"

[Manufacturer]
%Manufacturer%=Devices,NTamd64

[Devices.NTamd64]
"Eaton UPS"=LIBUSB_DEV, USB\VID_0463&PID_FFFF
"Eaton UPS"=LIBUSB_DEV, USB\VID_0463&PID_0001
"Eaton UPS (Powerware)"=LIBUSB_DEV, USB\VID_0592&PID_0002

[LIBUSB_DEV.NTamd64]
CopyFiles=LIBUSB_FILES

[LIBUSB_DEV.NTamd64.HW]
AddReg=LIBUSB_DEV.AddReg.HW

[LIBUSB_DEV.AddReg.HW]
HKR,,SurpriseRemovalOK,0x00010001,1

[LIBUSB_DEV.NTamd64.Services]
AddService=libusb0,0x00000002,LIBUSB_SVC

[LIBUSB_SVC]
DisplayName="libusb-win32 USB Driver"
ServiceType=1
StartType=3
ErrorControl=0
ServiceBinary=%12%\libusb0.sys

[LIBUSB_FILES]
libusb0.sys

[SourceDisksNames]
1="libusb-win32 Driver"

[SourceDisksFiles]
libusb0.sys=1

[DestinationDirs]
LIBUSB_FILES=12

[Strings]
Manufacturer="Eaton UPS (libusb-win32)"
"@

$driverDir = Join-Path $tempDir "driver"
New-Item -ItemType Directory -Force -Path $driverDir | Out-Null

# Copy the driver sys file
$sysSrc = Join-Path $extractDir "bin\amd64\libusb0.sys"
Copy-Item $sysSrc (Join-Path $driverDir "libusb0.sys") -Force

# Write the INF
$infPath = Join-Path $driverDir "eaton_ups.inf"
$infContent | Set-Content $infPath -Encoding ASCII

# Install the driver using pnputil
Write-Host "        Adding driver to Windows driver store..."
$result = pnputil /add-driver $infPath /install 2>&1
$resultText = $result -join "`n"
if ($resultText -match "successfully" -or $resultText -match "Published name") {
    Write-Host "        Driver installed." -ForegroundColor Green
} else {
    Write-Host "        pnputil output:" -ForegroundColor Yellow
    Write-Host $resultText
    Write-Host ""
    Write-Host "        If the driver didn't install, you may need to manually update" -ForegroundColor Yellow
    Write-Host "        the driver in Device Manager for the 'HID UPS Battery' device." -ForegroundColor Yellow
}

# Clean up
Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "  Setup complete!" -ForegroundColor Green
Write-Host ""
Write-Host "  If you just plugged in the UPS, unplug and re-plug the USB cable,"
Write-Host "  then run:  .\eaton-ups.exe status"
Write-Host ""
