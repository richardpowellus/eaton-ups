package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "status":
		cmdStatus()
	case "settings":
		cmdSettings()
	case "set":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: eaton-ups set <path> <value>")
			os.Exit(1)
		}
		cmdSet(os.Args[2], os.Args[3])
	case "raw":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: eaton-ups raw <reportID>")
			os.Exit(1)
		}
		cmdRaw(os.Args[2])
	case "describe":
		cmdDescribe()
	case "scan":
		cmdScan()
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Eaton 9PX UPS CLI — Direct USB HID control (no SetUPS needed)

Usage:
  eaton-ups status              Show live UPS status
  eaton-ups settings            Show all settings with current values
  eaton-ups set <path> <value>  Change a setting
  eaton-ups raw <reportID>      Read raw HID feature report (hex: 0x0D)
  eaton-ups describe            Show parsed HID report descriptor
  eaton-ups scan                Read all feature reports (raw hex dump)
  eaton-ups help                Show this help

Requires libusb0.dll (bundled or from Eaton SetUPS).`)
}

func openOrDie() *UPS {
	ups, err := OpenUPS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		fmt.Fprintln(os.Stderr, "Make sure:")
		fmt.Fprintln(os.Stderr, "  • Eaton 9PX UPS is connected via USB")
		fmt.Fprintln(os.Stderr, "  • libusb0 driver is installed (Eaton SetUPS installs it)")
		fmt.Fprintln(os.Stderr, "  • SetUPS is NOT running (close it first)")
		os.Exit(1)
	}
	return ups
}

func cmdStatus() {
	ups := openOrDie()
	defer ups.Close()

	items, err := ups.ReadStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("  Eaton 9PX UPS — Live Status")
	fmt.Println("  ═══════════════════════════")
	fmt.Println()
	for _, it := range items {
		if it.Unit != "" {
			fmt.Printf("  %-20s %s %s\n", it.Name, it.Value, it.Unit)
		} else {
			fmt.Printf("  %-20s %s\n", it.Name, it.Value)
		}
	}
	fmt.Println()
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

	if ups.desc == nil {
		fmt.Fprintln(os.Stderr, "Error: HID descriptor not available")
		os.Exit(1)
	}

	// Find field — try suffix match on Feature fields
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
	fmt.Printf("  Current: %d → New: %d\n", oldVal, value)

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
