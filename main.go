package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Friendly aliases for common settings
var settingAliases = map[string]struct {
	path   string
	report uint8   // non-zero = direct report read (not in Feature descriptor)
	offset int     // byte offset in direct report (after report ID)
	values map[string]int32 // friendly value names
}{
	"he-mode": {"", 0x4F, 0, map[string]int32{
		"on": 1, "off": 0, "enable": 1, "disable": 0, "enabled": 1, "disabled": 0,
	}},
	"deep-discharge-protection": {"UPS.BatterySystem.Battery.DeepDischargeProtection", 0, 0, map[string]int32{
		"on": 1, "off": 0, "enable": 1, "disable": 0,
	}},
	"audible-alarm": {"UPS.PowerSummary.AudibleAlarmControl", 0, 0, map[string]int32{
		"on": 2, "off": 1, "mute": 3, "enabled": 2, "disabled": 1,
	}},
	"output-voltage": {"UPS.Flow.ConfigVoltage", 0, 0, nil},
	"battery-test":   {"UPS.BatterySystem.Battery.Test", 0, 0, nil},
}

func main() {
	// Check for global flags
	jsonOutput := false
	args := os.Args[1:]
	filtered := args[:0]
	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) == 0 {
		printUsage()
		os.Exit(0)
	}

	switch args[0] {
	case "status":
		cmdStatus(jsonOutput)
	case "watch":
		interval := 2
		if len(args) > 1 {
			if v, err := strconv.Atoi(args[1]); err == nil && v > 0 {
				interval = v
			}
		}
		cmdWatch(interval)
	case "settings":
		cmdSettings()
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: eaton-ups set <name> <value>")
			fmt.Fprintln(os.Stderr, "\nAliases:")
			for alias := range settingAliases {
				fmt.Fprintf(os.Stderr, "  %s\n", alias)
			}
			fmt.Fprintln(os.Stderr, "\nOr use the full HID path from 'eaton-ups settings'")
			os.Exit(1)
		}
		cmdSet(args[1], args[2])
	case "raw":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: eaton-ups raw <reportID>")
			os.Exit(1)
		}
		cmdRaw(args[1])
	case "describe":
		cmdDescribe()
	case "scan":
		cmdScan()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Eaton 9PX UPS CLI - Direct USB HID control (no SetUPS needed)

Usage:
  eaton-ups status [--json]     Show live UPS status
  eaton-ups watch [seconds]     Auto-refreshing status (default: 2s)
  eaton-ups settings            Show all settings with current values
  eaton-ups set <name> <value>  Change a setting (alias or HID path)
  eaton-ups raw <reportID>      Read raw HID feature report (hex: 0x0D)
  eaton-ups describe            Show parsed HID report descriptor
  eaton-ups scan                Read all feature reports (raw hex dump)
  eaton-ups help                Show this help

Setting aliases:
  he-mode                  on/off   High Efficiency mode
  deep-discharge-protection on/off  Battery deep discharge protection
  audible-alarm            on/off/mute  Audible alarm control
  output-voltage           <volts>  Output voltage (e.g. 120)
  battery-test             <code>   Trigger battery test

Requires libusb0.dll (run setup.ps1 to install).`)
}

func openOrDie() *UPS {
	ups, err := OpenUPS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		fmt.Fprintln(os.Stderr, "Make sure:")
		fmt.Fprintln(os.Stderr, "  - Eaton 9PX UPS is connected via USB")
		fmt.Fprintln(os.Stderr, "  - libusb0 driver is installed (run setup.ps1)")
		fmt.Fprintln(os.Stderr, "  - SetUPS is NOT running (close it first)")
		os.Exit(1)
	}
	return ups
}

// ANSI color helpers
func color(code, text string) string { return "\033[" + code + "m" + text + "\033[0m" }
func green(s string) string          { return color("32", s) }
func yellow(s string) string         { return color("33", s) }
func red(s string) string            { return color("31", s) }
func cyan(s string) string           { return color("36", s) }
func dim(s string) string            { return color("90", s) }

func colorizeValue(name, value, unit string) string {
	full := value
	if unit != "" {
		full = value + " " + unit
	}
	switch {
	case name == "Load":
		if v, _ := strconv.Atoi(value); v >= 80 {
			return red(full)
		} else if v >= 50 {
			return yellow(full)
		}
		return green(full)
	case name == "Battery Charge":
		if v, _ := strconv.Atoi(value); v <= 20 {
			return red(full)
		} else if v <= 50 {
			return yellow(full)
		}
		return green(full)
	case name == "Runtime":
		if v, _ := strconv.ParseFloat(value, 64); v <= 5 {
			return red(full)
		} else if v <= 10 {
			return yellow(full)
		}
		return green(full)
	case name == "Efficiency":
		return green(full)
	case name == "HE Mode":
		if value == "Enabled" {
			return green(full)
		}
		return yellow(full)
	case strings.HasSuffix(name, "Temp"):
		if v, _ := strconv.ParseFloat(value, 64); v >= 45 {
			return red(full)
		} else if v >= 35 {
			return yellow(full)
		}
		return full
	}
	return full
}

func cmdStatus(jsonOut bool) {
	ups := openOrDie()
	defer ups.Close()

	items, err := ups.ReadStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		m := make(map[string]string)
		for _, it := range items {
			if it.Unit != "" {
				m[it.Name] = it.Value + " " + it.Unit
			} else {
				m[it.Name] = it.Value
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(m)
		return
	}

	printStatus(items)
}

func printStatus(items []StatusItem) {
	fmt.Println()
	fmt.Printf("  %s\n", cyan("Eaton 9PX UPS - Live Status"))
	fmt.Printf("  %s\n", cyan("==========================="))
	fmt.Println()
	for _, it := range items {
		fmt.Printf("  %-20s %s\n", it.Name, colorizeValue(it.Name, it.Value, it.Unit))
	}
	fmt.Println()
}

func cmdWatch(interval int) {
	ups := openOrDie()
	defer ups.Close()

	clearScreen := "\033[2J\033[H"
	for {
		items, err := ups.ReadStatus()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(clearScreen)
		printStatus(items)
		fmt.Printf("  %s\n", dim(fmt.Sprintf("Refreshing every %ds. Press Ctrl+C to stop.", interval)))
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func cmdSettings() {
	ups := openOrDie()
	defer ups.Close()

	settings := ups.ListSettings()
	if len(settings) == 0 {
		fmt.Println("No settings found")
		return
	}

	fmt.Println()
	fmt.Println("  Eaton 9PX UPS — Feature Report Fields")
	fmt.Println("  ═════════════════════════════════════")
	fmt.Println()

	// Group by report ID
	type group struct {
		id     uint8
		fields []ReportField
	}
	var groups []group
	seen := make(map[uint8]int)
	for _, f := range settings {
		if idx, ok := seen[f.ReportID]; ok {
			groups[idx].fields = append(groups[idx].fields, f)
		} else {
			seen[f.ReportID] = len(groups)
			groups = append(groups, group{f.ReportID, []ReportField{f}})
		}
	}

	for _, g := range groups {
		data, err := ups.GetFeatureReport(g.id)
		if err != nil {
			fmt.Printf("  Report 0x%02X: error: %v\n", g.id, err)
			continue
		}

		fmt.Printf("  ── Report 0x%02X (%d bytes) ──\n", g.id, len(data))
		for _, f := range g.fields {
			val := ExtractValue(data[1:], &f) // skip report ID byte
			rw := "RO"
			if f.IsWritable() {
				rw = "RW"
			}
			fmt.Printf("    [%s] %-50s = %-8d (%d..%d, %db)\n",
				rw, f.Path, val, f.LogicalMin, f.LogicalMax, f.Size)
		}
		fmt.Println()
	}
}

func cmdSet(pathArg string, valueStr string) {
	ups := openOrDie()
	defer ups.Close()

	// Check for friendly alias
	if alias, ok := settingAliases[strings.ToLower(pathArg)]; ok {
		// Resolve friendly value names
		if alias.values != nil {
			if v, ok := alias.values[strings.ToLower(valueStr)]; ok {
				valueStr = strconv.Itoa(int(v))
			}
		}

		// Direct report write (e.g. HE mode at report 0x4F)
		if alias.report != 0 {
			value, err := strconv.ParseInt(valueStr, 0, 32)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid value '%s'\n", valueStr)
				os.Exit(1)
			}

			// Read current
			data, err := ups.GetFeatureReport(alias.report)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading: %v\n", err)
				os.Exit(1)
			}
			oldVal := int32(0)
			if len(data) > alias.offset+1 {
				oldVal = int32(data[alias.offset+1])
			}

			fmt.Printf("  %s\n", pathArg)
			fmt.Printf("  Current: %d -> New: %d\n", oldVal, value)

			if len(data) > alias.offset+1 {
				data[alias.offset+1] = byte(value)
			}
			if err := ups.SetFeatureReport(alias.report, data); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			// Verify
			data2, _ := ups.GetFeatureReport(alias.report)
			if len(data2) > alias.offset+1 {
				fmt.Printf("  Verified: %d\n", data2[alias.offset+1])
			}
			return
		}

		// Path-based alias
		pathArg = alias.path
	}

	if ups.desc == nil {
		fmt.Fprintln(os.Stderr, "Error: HID descriptor not available")
		os.Exit(1)
	}

	// Find field by path
	var field *ReportField
	for i := range ups.desc.Fields {
		f := &ups.desc.Fields[i]
		if f.ReportType != "Feature" {
			continue
		}
		if strings.EqualFold(f.Path, pathArg) || strings.HasSuffix(strings.ToLower(f.Path), strings.ToLower(pathArg)) {
			field = f
			break
		}
	}
	if field == nil {
		fmt.Fprintf(os.Stderr, "Error: field '%s' not found\n", pathArg)
		fmt.Fprintln(os.Stderr, "Run 'eaton-ups settings' to see available fields")
		os.Exit(1)
	}

	value, err := strconv.ParseInt(valueStr, 0, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid value '%s': %v\n", valueStr, err)
		os.Exit(1)
	}

	if !field.IsWritable() {
		fmt.Fprintf(os.Stderr, "Warning: field marked read-only, attempting write anyway...\n")
	}

	oldVal, _ := ups.ReadField(field)
	fmt.Printf("  %s\n", field.Path)
	fmt.Printf("  Current: %d -> New: %d\n", oldVal, value)

	if err := ups.WriteField(field, int32(value)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	newVal, err := ups.ReadField(field)
	if err != nil {
		fmt.Println("  Written (could not verify)")
	} else {
		fmt.Printf("  Verified: %d\n", newVal)
	}
}

func cmdRaw(reportIDStr string) {
	ups := openOrDie()
	defer ups.Close()

	id, err := strconv.ParseUint(reportIDStr, 0, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid report ID '%s'\n", reportIDStr)
		os.Exit(1)
	}

	data, err := ups.GetFeatureReport(uint8(id))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Report 0x%02X (%d bytes):\n", id, len(data))
	for i, b := range data {
		if i > 0 && i%16 == 0 {
			fmt.Println()
		}
		fmt.Printf("%02X ", b)
	}
	fmt.Println()

	if ups.desc != nil {
		fields := ups.desc.FieldsByReportID(uint8(id), "Feature")
		if len(fields) > 0 {
			fmt.Println("\nDecoded (skipping report ID byte):")
			for _, f := range fields {
				val := ExtractValue(data[1:], &f)
				rw := "RO"
				if f.IsWritable() {
					rw = "RW"
				}
				fmt.Printf("  [%s] %-50s = %d\n", rw, f.Path, val)
			}
		}
	}
}

func cmdDescribe() {
	ups := openOrDie()
	defer ups.Close()

	if ups.desc == nil {
		fmt.Fprintln(os.Stderr, "Error: HID descriptor not available")
		os.Exit(1)
	}

	// Deduplicate
	seen := make(map[string]bool)
	var fields []ReportField
	for _, f := range ups.desc.Fields {
		key := fmt.Sprintf("%s:%d:%d:%d:%s", f.ReportType, f.ReportID, f.Offset, f.Size, f.Path)
		if !seen[key] {
			seen[key] = true
			fields = append(fields, f)
		}
	}

	fmt.Printf("HID Report Descriptor: %d unique fields\n\n", len(fields))

	lastHeader := ""
	for _, f := range fields {
		hdr := fmt.Sprintf("Report 0x%02X (%s)", f.ReportID, f.ReportType)
		if hdr != lastHeader {
			lastHeader = hdr
			fmt.Printf("── %s ──\n", hdr)
		}
		rw := "RO"
		if f.IsWritable() {
			rw = "RW"
		}
		fmt.Printf("  [%s] %-55s bits[%d:%d] range[%d..%d]\n",
			rw, f.Path, f.Offset, f.Offset+f.Size, f.LogicalMin, f.LogicalMax)
	}
}

func cmdScan() {
	ups := openOrDie()
	defer ups.Close()

	fmt.Println("Scanning all feature reports (1-255)...\n")
	count := 0
	for id := 1; id < 256; id++ {
		data, err := ups.GetFeatureReport(uint8(id))
		if err != nil || len(data) == 0 {
			continue
		}
		count++
		fmt.Printf("  0x%02X (%2d bytes): ", id, len(data))
		for _, b := range data {
			fmt.Printf("%02X ", b)
		}
		fmt.Println()
	}
	fmt.Printf("\n  %d reports found\n", count)
}
