package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	eatonVID = 0x0463
	eatonPID = 0xFFFF
)

// libusb0 API
var (
	libusb0            *syscall.LazyDLL
	pUsbInit           *syscall.LazyProc
	pUsbFindBusses     *syscall.LazyProc
	pUsbFindDevices    *syscall.LazyProc
	pUsbGetBusses      *syscall.LazyProc
	pUsbOpen           *syscall.LazyProc
	pUsbClose          *syscall.LazyProc
	pUsbClaimInterface *syscall.LazyProc
	pUsbControlMsg     *syscall.LazyProc
	pUsbGetStrSimple   *syscall.LazyProc
)

func initLibUSB() error {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	paths := []string{
		filepath.Join(exeDir, "libusb0.dll"),
		`C:\Program Files (x86)\Eaton\SetUPS\bin\libusb0.dll`,
		`libusb0.dll`,
	}
	var lastErr error
	for _, p := range paths {
		libusb0 = syscall.NewLazyDLL(p)
		lastErr = libusb0.Load()
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return fmt.Errorf("cannot load libusb0.dll: %w", lastErr)
	}
	pUsbInit = libusb0.NewProc("usb_init")
	pUsbFindBusses = libusb0.NewProc("usb_find_busses")
	pUsbFindDevices = libusb0.NewProc("usb_find_devices")
	pUsbGetBusses = libusb0.NewProc("usb_get_busses")
	pUsbOpen = libusb0.NewProc("usb_open")
	pUsbClose = libusb0.NewProc("usb_close")
	pUsbClaimInterface = libusb0.NewProc("usb_claim_interface")
	pUsbControlMsg = libusb0.NewProc("usb_control_msg")
	pUsbGetStrSimple = libusb0.NewProc("usb_get_string_simple")
	pUsbInit.Call()
	return nil
}

// libusb0 struct offsets — the Windows build uses 512-byte dirname/filename fields.
// Offsets are computed based on compile-time pointer size.
const ptrSize = unsafe.Sizeof(uintptr(0))

var (
	// usb_bus:  next(ptr) + prev(ptr) + dirname(512) → devices(ptr)
	busDevicesOff = uintptr(ptrSize + ptrSize + 512)
	// usb_device: next(ptr) + prev(ptr) + filename(512) + bus(ptr) → descriptor
	devDescOff = uintptr(ptrSize + ptrSize + 512 + ptrSize)
)

// UPS represents a connected Eaton UPS device
type UPS struct {
	handle uintptr
	desc   *HIDDescriptor
	ifNum  int
}

func OpenUPS() (*UPS, error) {
	if err := initLibUSB(); err != nil {
		return nil, err
	}
	pUsbFindBusses.Call()
	pUsbFindDevices.Call()
	busPtr, _, _ := pUsbGetBusses.Call()
	if busPtr == 0 {
		return nil, fmt.Errorf("no USB busses found")
	}
	for bp := busPtr; bp != 0; bp = *(*uintptr)(unsafe.Pointer(bp)) {
		devPtr := *(*uintptr)(unsafe.Pointer(bp + busDevicesOff))
		for dp := devPtr; dp != 0; dp = *(*uintptr)(unsafe.Pointer(dp)) {
			vid := *(*uint16)(unsafe.Pointer(dp + devDescOff + 8))
			pid := *(*uint16)(unsafe.Pointer(dp + devDescOff + 10))
			if vid == eatonVID && pid == eatonPID {
				handle, _, _ := pUsbOpen.Call(dp)
				if handle == 0 {
					continue
				}
				ups := &UPS{handle: handle}
				pUsbClaimInterface.Call(handle, 0)
				if err := ups.readHIDDescriptor(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				}
				return ups, nil
			}
		}
	}
	return nil, fmt.Errorf("Eaton UPS (%04x:%04x) not found", eatonVID, eatonPID)
}

func (u *UPS) readHIDDescriptor() error {
	buf := make([]byte, 4096)
	n := u.control(0x81, 0x06, 0x2200, uint16(u.ifNum), buf, 5000)
	if n <= 0 {
		return fmt.Errorf("failed to read HID report descriptor (ret=%d)", n)
	}
	desc, err := ParseHIDDescriptor(buf[:n])
	if err != nil {
		return err
	}
	u.desc = desc
	return nil
}

func (u *UPS) control(reqType uint8, req uint8, value, index uint16, data []byte, timeout int) int {
	var p uintptr
	if len(data) > 0 {
		p = uintptr(unsafe.Pointer(&data[0]))
	}
	ret, _, _ := pUsbControlMsg.Call(u.handle,
		uintptr(reqType), uintptr(req), uintptr(value), uintptr(index),
		p, uintptr(len(data)), uintptr(timeout))
	return int(int32(ret))
}

func (u *UPS) Close() {
	if u.handle != 0 {
		pUsbClose.Call(u.handle)
		u.handle = 0
	}
}

// GetFeatureReport reads a HID feature report.
// Returns raw data where byte 0 is the report ID.
func (u *UPS) GetFeatureReport(reportID uint8) ([]byte, error) {
	buf := make([]byte, 128)
	n := u.control(0xA1, 0x01, uint16(0x0300)|uint16(reportID), uint16(u.ifNum), buf, 5000)
	if n < 0 {
		return nil, fmt.Errorf("GET_REPORT 0x%02X failed (ret=%d)", reportID, n)
	}
	return buf[:n], nil
}

func (u *UPS) SetFeatureReport(reportID uint8, data []byte) error {
	n := u.control(0x21, 0x09, uint16(0x0300)|uint16(reportID), uint16(u.ifNum), data, 5000)
	if n < 0 {
		return fmt.Errorf("SET_REPORT 0x%02X failed (ret=%d)", reportID, n)
	}
	return nil
}

func (u *UPS) GetString(index int) string {
	if index <= 0 {
		return ""
	}
	buf := make([]byte, 256)
	ret, _, _ := pUsbGetStrSimple.Call(u.handle, uintptr(index),
		uintptr(unsafe.Pointer(&buf[0])), 256)
	n := int(int32(ret))
	if n <= 0 {
		return ""
	}
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return string(buf[:i])
		}
	}
	return string(buf[:n])
}

// ReadField reads a field value. Skips byte 0 (report ID) before extracting.
func (u *UPS) ReadField(field *ReportField) (int32, error) {
	data, err := u.GetFeatureReport(field.ReportID)
	if err != nil {
		return 0, err
	}
	if len(data) < 2 {
		return 0, fmt.Errorf("report 0x%02X too short (%d bytes)", field.ReportID, len(data))
	}
	return ExtractValue(data[1:], field), nil
}

func (u *UPS) WriteField(field *ReportField, value int32) error {
	data, err := u.GetFeatureReport(field.ReportID)
	if err != nil {
		return fmt.Errorf("read current: %w", err)
	}
	if len(data) < 2 {
		return fmt.Errorf("report too short")
	}
	PackValue(data[1:], field, value)
	return u.SetFeatureReport(field.ReportID, data)
}

// StatusItem is a display line in the status output
type StatusItem struct {
	Name  string
	Value string
	Unit  string
}

func (u *UPS) ReadStatus() ([]StatusItem, error) {
	var items []StatusItem
	if s := u.GetString(2); s != "" {
		items = append(items, StatusItem{"Product", s, ""})
	}
	if s := u.GetString(3); s != "" {
		items = append(items, StatusItem{"Model", s, ""})
	}
	if s := u.GetString(4); s != "" {
		items = append(items, StatusItem{"Serial", s, ""})
	}
	if u.desc == nil {
		return items, nil
	}

	type q struct {
		name, path, unit string
		div              float64
		fmt              string
		str              bool
	}
	qs := []q{
		{"Firmware", "PowerSummary.iVersion", "", 0, "", true},
		{"Input Voltage", "PowerConverter.Input.Voltage", "V", 10, "%.1f", false},
		{"Output Voltage", "PowerConverter.Output.Voltage", "V", 10, "%.1f", false},
		{"Output Current", "PowerConverter.Output.Current", "A", 10, "%.1f", false},
		{"Frequency", "PowerConverter.Output.Frequency", "Hz", 10, "%.1f", false},
		{"Load", "PowerSummary.PercentLoad", "%", 1, "%.0f", false},
		{"Active Power", "PowerConverter.Output.ActivePower", "W", 1, "%.0f", false},
		{"Apparent Power", "PowerConverter.Output.ApparentPower", "VA", 1, "%.0f", false},
		{"Efficiency", "PowerConverter.Output.0x0069", "%", 1, "%.0f", false},
		{"Battery Voltage", "PowerSummary.Voltage", "V", 10, "%.1f", false},
		{"Battery Charge", "PowerSummary.RemainingCapacity", "%", 1, "%.0f", false},
		{"Runtime", "PowerSummary.RunTimeToEmpty", "", 0, "", false},
		{"Internal Temp", "PowerSummary.Temperature", "", 10, "", false},
		{"Inverter Temp", "PowerConverter.Inverter.Temperature", "", 10, "", false},
		{"Rated Power", "Flow.ConfigActivePower", "W", 1, "%.0f", false},
		{"Rated VA", "Flow.ConfigApparentPower", "VA", 1, "%.0f", false},
		{"HE Mode", "PowerConverter.Input.Switchable", "", 1, "", false},
	}
	for _, q := range qs {
		f := u.findFeature(q.path)
		if f == nil {
			// Input Voltage isn't in Feature reports -- read report 0x3A directly
			if q.name == "Input Voltage" {
				if data, err := u.GetFeatureReport(0x3A); err == nil && len(data) >= 7 {
					raw := uint16(data[5]) | uint16(data[6])<<8
					items = append(items, StatusItem{q.name,
						fmt.Sprintf("%.1f", float64(raw)/10), q.unit})
				}
			}
			// HE Mode isn't in Feature reports -- read report 0x4F directly
			if q.name == "HE Mode" {
				if data, err := u.GetFeatureReport(0x4F); err == nil && len(data) >= 2 {
					v := "Disabled"
					if data[1] != 0 {
						v = "Enabled"
					}
					items = append(items, StatusItem{q.name, v, ""})
				}
			}
			continue
		}
		val, err := u.ReadField(f)
		if err != nil {
			continue
		}
		if q.str {
			if s := u.GetString(int(val)); s != "" {
				items = append(items, StatusItem{q.name, s, ""})
			}
		} else if q.name == "Runtime" {
			mins := float64(val) / 60.0
			items = append(items, StatusItem{q.name, fmt.Sprintf("%.1f", mins), "min"})
		} else if q.name == "HE Mode" {
			v := "Disabled"
			if val != 0 {
				v = "Enabled"
			}
			items = append(items, StatusItem{q.name, v, ""})
		} else if strings.HasSuffix(q.name, "Temp") {
			celsius := float64(val) / q.div
			items = append(items, StatusItem{q.name, fmt.Sprintf("%.1f", celsius), "C"})
		} else if q.div > 1 {
			items = append(items, StatusItem{q.name,
				fmt.Sprintf(q.fmt, float64(val)/q.div), q.unit})
		} else {
			items = append(items, StatusItem{q.name,
				fmt.Sprintf(q.fmt, float64(val)), q.unit})
		}
	}
	return items, nil
}

// findFeature finds a Feature-type field by path suffix
func (u *UPS) findFeature(path string) *ReportField {
	if u.desc == nil {
		return nil
	}
	for i := range u.desc.Fields {
		f := &u.desc.Fields[i]
		if f.ReportType != "Feature" {
			continue
		}
		if strings.HasSuffix(f.Path, path) {
			return f
		}
	}
	parts := strings.Split(path, ".")
	if len(parts) >= 2 {
		suffix := parts[len(parts)-2] + "." + parts[len(parts)-1]
		for i := range u.desc.Fields {
			f := &u.desc.Fields[i]
			if f.ReportType != "Feature" {
				continue
			}
			if strings.HasSuffix(f.Path, suffix) {
				return f
			}
		}
	}
	return nil
}

// ListSettings returns deduplicated feature report fields
func (u *UPS) ListSettings() []ReportField {
	if u.desc == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []ReportField
	for _, f := range u.desc.Fields {
		if f.ReportType != "Feature" {
			continue
		}
		key := fmt.Sprintf("%d:%d:%d:%s", f.ReportID, f.Offset, f.Size, f.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}
