# eaton-ups

CLI tool for monitoring and controlling Eaton 9PX UPS devices over USB — no Eaton software required.

Communicates directly with the UPS via USB HID using the libusb0 driver, parsing the HID report descriptor to read status values and modify settings.

## Features

- **Live status** — voltage, current, load, battery charge, runtime, temperature
- **Settings** — read and write all HID feature report fields
- **Raw access** — read/decode individual HID reports
- **HID descriptor** — full parsed dump of the device's report descriptor
- **No dependencies** — pure Go + syscall (no CGO), single static binary + DLL

## Supported Hardware

Tested with:
- **Eaton 9PX 1000 RT** (VID `0463`, PID `FFFF`)

Should work with other Eaton/MGE UPS models that use the same USB HID Power Device class protocol (most 9PX, 9SX, 5PX, 5P models).

## Prerequisites

The UPS must be connected via USB with the **libusb0** driver installed. Run the included setup script (as Administrator) to install everything automatically — no Eaton software needed:

```powershell
# Run as Administrator
.\setup.ps1
```

This downloads [libusb-win32](https://github.com/mcuee/libusb-win32/releases) (LGPL), installs the USB driver and device filter for Eaton UPS devices, and places `libusb0.dll` next to the executable.

<details>
<summary>Manual setup (without the script)</summary>

1. Download [libusb-win32](https://github.com/mcuee/libusb-win32/releases) and extract the zip
2. Copy `bin\amd64\libusb0.dll` next to `eaton-ups.exe`
3. Install the driver for your UPS:
   - Open **Device Manager**
   - Find the UPS under "Batteries" → "HID UPS Battery"
   - Right-click → Update Driver → Browse → pick the libusb-win32 `bin\amd64` folder
   - Or if you already have Eaton SetUPS installed, the driver is already in place
</details>

## Usage

```
eaton-ups status              Show live UPS status
eaton-ups settings            Show all settings with current values
eaton-ups set <path> <value>  Change a setting
eaton-ups raw <reportID>      Read raw HID feature report (hex: 0x0D)
eaton-ups describe            Show parsed HID report descriptor
eaton-ups scan                Read all feature reports (raw hex dump)
```

### Example: Status

```
  Eaton 9PX UPS — Live Status
  ═══════════════════════════
  Product              Eaton 9PX
  Model                1000 RT
  Serial               GA17T26013
  Input Voltage        119.4 V
  Output Voltage       119.0 V
  Output Current       2.8 A
  Frequency            60.0 Hz
  Load                 32 %
  Active Power         290 W
  Apparent Power       290 VA
  Battery Voltage      41.9 V
  Battery Charge       91 %
  Runtime              28.6 min
```

### Example: Read a raw report

```
> eaton-ups raw 0x42

Report 0x42 (19 bytes):
42 01 01 29 01 22 01 2C 01 19 00 5F 58 02 61 A4 04 01 01

Decoded (skipping report ID byte):
  [RO] UPS.PowerConverter.Chopper.Temperature             = 257
  [RO] UPS.PowerConverter.Inverter.Temperature            = 297
  [RO] UPS.PowerConverter.Output.ActivePower              = 290
  [RO] UPS.PowerConverter.Output.ApparentPower            = 300
  [RO] UPS.PowerConverter.Output.Current                  = 25
  [RO] UPS.PowerConverter.Output.Frequency                = 600
  [RO] UPS.PowerConverter.Output.Voltage                  = 1188
  [RO] UPS.PowerConverter.Rectifier.Temperature           = 257
```

### Example: Change a setting

```
> eaton-ups set UPS.BatterySystem.Battery.DeepDischargeProtection 1

  UPS.BatterySystem.Battery.DeepDischargeProtection
  Current: 1 → New: 1
  Verified: 1
```

## Building

```
go build -o eaton-ups.exe .
```

Requires Go 1.26+. No CGO needed — the app loads `libusb0.dll` at runtime via syscall.

## Architecture

| File | Purpose |
|------|---------|
| `ups.go` | USB device discovery and communication via libusb0 syscalls |
| `hid.go` | HID report descriptor parser and value extraction |
| `usages.go` | USB HID usage name tables (Power Device, Battery System, MGE vendor) |
| `main.go` | CLI commands |
| `setup.ps1` | One-step driver + DLL installer (run as Admin) |
| `libusb0.dll` | 64-bit libusb-win32 runtime ([download separately](https://github.com/mcuee/libusb-win32/releases) or run `setup.ps1`) |

The app works by:
1. Finding the UPS via libusb0 device enumeration (VID `0463`, PID `FFFF`)
2. Reading the 1215-byte HID report descriptor via `GET_DESCRIPTOR`
3. Parsing it into ~380 typed fields with usage paths like `UPS.PowerSummary.Voltage`
4. Reading/writing values via `GET_REPORT` / `SET_REPORT` control transfers

## License

MIT
