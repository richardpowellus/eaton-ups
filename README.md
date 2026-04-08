# eaton-ups

CLI tool for monitoring and controlling Eaton 9PX UPS devices over USB — no Eaton software required.

Communicates directly with the UPS via USB HID using the libusb0 driver, parsing the HID report descriptor to read status values and modify settings.

## Quick Start

```powershell
.\setup.ps1              # one-time: installs USB driver (self-elevates to Admin)
.\eaton-ups.exe status   # show live UPS data
```

## Status

Real-time view of everything the UPS reports:

```
> eaton-ups status

  Eaton 9PX UPS - Live Status
  ===========================

  Product              Eaton 9PX
  Model                1000 RT
  Serial               GA17T26013
  Input Voltage        119.9 V
  Output Voltage       119.8 V
  Output Current       2.5 A
  Frequency            60.0 Hz
  Load                 32 %
  Active Power         290 W
  Apparent Power       300 VA
  Efficiency           97 %
  Battery Voltage      41.9 V
  Battery Charge       91 %
  Runtime              27.6 min
  Internal Temp        29.3 C
  Inverter Temp        29.7 C
  Rated Power          900 W
  Rated VA             1000 VA
  HE Mode              Enabled
```

## Settings

Read every configurable field from the UPS, grouped by HID report. Shows whether each field is read-only (`RO`) or writable (`RW`), its current value, valid range, and bit width:

```
> eaton-ups settings

  -- Report 0x2A (2 bytes) --
    [RW] UPS.BatterySystem.Battery.DeepDischargeProtection  = 1        (0..255, 8b)

  -- Report 0x25 (5 bytes) --
    [RW] UPS.BatterySystem.Battery.TestPeriod               = 2592000  (0..2147483647, 32b)

  -- Report 0x42 (19 bytes) --
    [RO] UPS.PowerConverter.Chopper.Temperature             = 256      (0..65535, 16b)
    [RO] UPS.PowerConverter.Inverter.Temperature            = 297      (0..65535, 16b)
    [RO] UPS.PowerConverter.Output.ActivePower              = 290      (0..65535, 16b)
    [RO] UPS.PowerConverter.Output.Voltage                  = 1199     (0..65535, 16b)
    ...
```

## Changing Settings

Write any `[RW]` field by path:

```
> eaton-ups set UPS.BatterySystem.Battery.DeepDischargeProtection 0

  UPS.BatterySystem.Battery.DeepDischargeProtection
  Current: 1 -> New: 0
  Verified: 0
```

## Raw Reports

Read any HID feature report by ID and see both the raw hex and decoded fields:

```
> eaton-ups raw 0x42

Report 0x42 (19 bytes):
42 00 01 29 01 2C 01 2C 01 19 00 5F 58 02 61 B1 04 00 01

Decoded (skipping report ID byte):
  [RO] UPS.PowerConverter.Chopper.Temperature             = 256
  [RO] UPS.PowerConverter.Inverter.Temperature            = 297
  [RO] UPS.PowerConverter.Output.ActivePower              = 300
  [RO] UPS.PowerConverter.Output.ApparentPower            = 300
  [RO] UPS.PowerConverter.Output.Current                  = 25
  [RO] UPS.PowerConverter.Output.Frequency                = 600
  [RO] UPS.PowerConverter.Output.Voltage                  = 1201
  [RO] UPS.PowerConverter.Rectifier.Temperature           = 256
```

## Scan

Dump every readable report from the UPS (useful for reverse-engineering):

```
> eaton-ups scan

Scanning all feature reports (1-255)...

  0x01 ( 7 bytes): 01 01 00 01 00 00 01
  0x06 ( 6 bytes): 06 5B 3D 06 00 00
  0x07 (18 bytes): 07 00 00 00 00 00 00 07 21 02 25 01 1F F2 03 00 A3 01
  0x0C ( 5 bytes): 0C 01 02 64 64
  ...

  88 reports found
```

## HID Descriptor

Parse and display the full USB HID report descriptor — all 381 fields with their usage paths, bit layouts, and access types:

```
> eaton-ups describe

HID Report Descriptor: 381 unique fields

-- Report 0x2A (Feature) --
  [RW] UPS.BatterySystem.Battery.DeepDischargeProtection       bits[0:8] range[0..255]
-- Report 0x23 (Feature) --
  [RW] UPS.BatterySystem.Battery.DesignCapacity                bits[0:32] range[0..2147483647]
-- Report 0x42 (Feature) --
  [RO] UPS.PowerConverter.Chopper.Temperature                  bits[0:16] range[0..65535]
  [RO] UPS.PowerConverter.Inverter.Temperature                 bits[16:32] range[0..65535]
  [RO] UPS.PowerConverter.Output.ActivePower                   bits[32:48] range[0..65535]
  ...
```

## Supported Hardware

Tested with:
- **Eaton 9PX 1000 RT** (VID `0463`, PID `FFFF`)

Should work with other Eaton/MGE UPS models that use the same USB HID Power Device class protocol (most 9PX, 9SX, 5PX, 5P models).

## Setup

The UPS must be connected via USB with the **libusb0** driver installed. Run the included setup script to install everything automatically — no Eaton software needed:

```powershell
.\setup.ps1
```

The script self-elevates to Administrator, downloads the latest [libusb-win32](https://github.com/mcuee/libusb-win32/releases) (LGPL) from GitHub, installs the USB driver, and places `libusb0.dll` next to the executable.

<details>
<summary>Manual setup (without the script)</summary>

1. Download [libusb-win32](https://github.com/mcuee/libusb-win32/releases) and extract the zip
2. Copy `bin\amd64\libusb0.dll` next to `eaton-ups.exe`
3. Copy `bin\amd64\libusb0.sys` to `C:\Windows\System32\drivers\`
4. Run `bin\amd64\install-filter.exe install --device=USB\VID_0463&PID_FFFF`
</details>

## Building

```
go build -o eaton-ups.exe .
```

Requires Go 1.26+. No CGO needed — the app loads `libusb0.dll` at runtime via syscall.

## How It Works

| File | Purpose |
|------|---------|
| `ups.go` | USB device discovery and communication via libusb0 syscalls |
| `hid.go` | HID report descriptor parser and value extraction |
| `usages.go` | USB HID usage name tables (Power Device, Battery System, MGE vendor) |
| `main.go` | CLI commands |
| `setup.ps1` | One-step driver + DLL installer (self-elevates to Admin) |

The app:
1. Finds the UPS via libusb0 device enumeration (VID `0463`, PID `FFFF`)
2. Reads the 1215-byte HID report descriptor via `GET_DESCRIPTOR`
3. Parses it into ~380 typed fields with usage paths like `UPS.PowerSummary.Voltage`
4. Reads/writes values via `GET_REPORT` / `SET_REPORT` USB control transfers

No external Go dependencies. Works on both 32-bit and 64-bit Windows.

## License

MIT
