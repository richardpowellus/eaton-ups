package main

import (
	"encoding/binary"
	"fmt"
)

// HID report descriptor item tags
const (
	itemMain   = 0
	itemGlobal = 1
	itemLocal  = 2
)

const (
	mainInput         = 0x08
	mainOutput        = 0x09
	mainFeature       = 0x0B
	mainCollection    = 0x0A
	mainEndCollection = 0x0C
)

const (
	globalUsagePage   = 0x00
	globalLogicalMin  = 0x01
	globalLogicalMax  = 0x02
	globalReportSize  = 0x07
	globalReportID    = 0x08
	globalReportCount = 0x09
)

const (
	localUsage    = 0x00
	localUsageMin = 0x01
	localUsageMax = 0x02
)

// ReportField represents a single field in an HID report
type ReportField struct {
	Path       string
	ReportID   uint8
	ReportType string // "Input", "Output", or "Feature"
	Offset     int    // Bit offset within report data (after report ID byte)
	Size       int    // Bit size
	LogicalMin int32
	LogicalMax int32
	UsagePage  uint32
	UsageID    uint32
	Flags      uint32
}

func (f *ReportField) IsWritable() bool {
	return f.ReportType == "Feature" && (f.Flags&0x01) == 0
}

// HIDDescriptor holds parsed fields
type HIDDescriptor struct {
	Fields []ReportField
}

type globalState struct {
	usagePage   uint32
	logicalMin  int32
	logicalMax  int32
	reportSize  uint32
	reportID    uint8
	reportCount uint32
}

func readSigned(data []byte, size int) int32 {
	switch size {
	case 1:
		return int32(int8(data[0]))
	case 2:
		return int32(int16(binary.LittleEndian.Uint16(data)))
	case 4:
		return int32(binary.LittleEndian.Uint32(data))
	}
	return 0
}

func readUnsigned(data []byte, size int) uint32 {
	switch size {
	case 1:
		return uint32(data[0])
	case 2:
		return uint32(binary.LittleEndian.Uint16(data))
	case 4:
		return binary.LittleEndian.Uint32(data)
	}
	return 0
}

// ParseHIDDescriptor parses a raw HID report descriptor
func ParseHIDDescriptor(desc []byte) (*HIDDescriptor, error) {
	hid := &HIDDescriptor{}
	var gs globalState
	var usages []uint32
	var collections []string
	// Track bit offsets per (reportType, reportID) pair
	offsets := make(map[string]int)

	pos := 0
	for pos < len(desc) {
		b := desc[pos]
		dataSize := int(b & 0x03)
		if dataSize == 3 {
			dataSize = 4
		}
		iType := int((b >> 2) & 0x03)
		tag := int((b >> 4) & 0x0F)
		hdrSize := 1

		if b == 0xFE { // long item
			if pos+2 >= len(desc) {
				break
			}
			dataSize = int(desc[pos+1])
			hdrSize = 3
		}

		if pos+hdrSize+dataSize > len(desc) {
			break
		}
		itemData := desc[pos+hdrSize : pos+hdrSize+dataSize]
		pos += hdrSize + dataSize

		switch iType {
		case itemGlobal:
			switch tag {
			case globalUsagePage:
				gs.usagePage = readUnsigned(itemData, dataSize)
			case globalLogicalMin:
				gs.logicalMin = readSigned(itemData, dataSize)
			case globalLogicalMax:
				gs.logicalMax = readSigned(itemData, dataSize)
			case globalReportSize:
				gs.reportSize = readUnsigned(itemData, dataSize)
			case globalReportID:
				gs.reportID = uint8(readUnsigned(itemData, dataSize))
			case globalReportCount:
				gs.reportCount = readUnsigned(itemData, dataSize)
			}

		case itemLocal:
			switch tag {
			case localUsage, localUsageMin:
				usages = append(usages, readUnsigned(itemData, dataSize))
			}

		case itemMain:
			switch tag {
			case mainCollection:
				uid := uint32(0)
				if len(usages) > 0 {
					uid = usages[0]
				}
				usages = nil
				name := UsageName(gs.usagePage, uid)
				if name == "" {
					name = fmt.Sprintf("0x%04X", uid)
				}
				collections = append(collections, name)

			case mainEndCollection:
				if len(collections) > 0 {
					collections = collections[:len(collections)-1]
				}

			case mainInput, mainOutput, mainFeature:
				flags := readUnsigned(itemData, dataSize)
				rtype := "Input"
				if tag == mainOutput {
					rtype = "Output"
				} else if tag == mainFeature {
					rtype = "Feature"
				}

				offKey := fmt.Sprintf("%s:%d", rtype, gs.reportID)
				bitOff := offsets[offKey]

				for i := 0; i < int(gs.reportCount); i++ {
					uid := uint32(0)
					if i < len(usages) {
						uid = usages[i]
					} else if len(usages) > 0 {
						uid = usages[len(usages)-1]
					}

					path := ""
					for _, c := range collections {
						if path != "" {
							path += "."
						}
						path += c
					}
					uname := UsageName(gs.usagePage, uid)
					if uname == "" {
						uname = fmt.Sprintf("0x%04X", uid)
					}
					if path != "" {
						path += "."
					}
					path += uname

					hid.Fields = append(hid.Fields, ReportField{
						Path:       path,
						ReportID:   gs.reportID,
						ReportType: rtype,
						Offset:     bitOff,
						Size:       int(gs.reportSize),
						LogicalMin: gs.logicalMin,
						LogicalMax: gs.logicalMax,
						UsagePage:  gs.usagePage,
						UsageID:    uid,
						Flags:      flags,
					})
					bitOff += int(gs.reportSize)
				}
				offsets[offKey] = bitOff
				usages = nil
			}
		}
	}
	return hid, nil
}

// FindField looks up a field by exact path
func (h *HIDDescriptor) FindField(path string) *ReportField {
	for i := range h.Fields {
		if h.Fields[i].Path == path {
			return &h.Fields[i]
		}
	}
	return nil
}

// FieldsByReportID returns fields for a given report ID and type
func (h *HIDDescriptor) FieldsByReportID(reportID uint8, reportType string) []ReportField {
	seen := make(map[string]bool)
	var result []ReportField
	for _, f := range h.Fields {
		if f.ReportID == reportID && f.ReportType == reportType {
			key := fmt.Sprintf("%d:%s", f.Offset, f.Path)
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, f)
		}
	}
	return result
}

// ReportIDs returns unique report IDs for a given type
func (h *HIDDescriptor) ReportIDs(reportType string) []uint8 {
	seen := make(map[uint8]bool)
	var ids []uint8
	for _, f := range h.Fields {
		if f.ReportType == reportType && !seen[f.ReportID] {
			seen[f.ReportID] = true
			ids = append(ids, f.ReportID)
		}
	}
	return ids
}

// ExtractValue extracts a field's integer value from report data
// (data should NOT include the report ID byte)
func ExtractValue(data []byte, field *ReportField) int32 {
	byteOff := field.Offset / 8
	bitOff := field.Offset % 8
	if byteOff >= len(data) {
		return 0
	}
	needed := (bitOff + field.Size + 7) / 8
	var raw uint64
	for i := 0; i < needed && byteOff+i < len(data); i++ {
		raw |= uint64(data[byteOff+i]) << (uint(i) * 8)
	}
	raw >>= uint(bitOff)
	mask := uint64((1 << uint(field.Size)) - 1)
	val := raw & mask

	if field.LogicalMin < 0 {
		sign := uint64(1 << uint(field.Size-1))
		if val&sign != 0 {
			val |= ^mask
		}
		return int32(int64(val))
	}
	return int32(val)
}

// PackValue writes a value into report data at the field's position
func PackValue(data []byte, field *ReportField, value int32) {
	byteOff := field.Offset / 8
	bitOff := field.Offset % 8
	mask := uint64((1 << uint(field.Size)) - 1)
	val := uint64(value) & mask

	needed := (bitOff + field.Size + 7) / 8
	for i := 0; i < needed && byteOff+i < len(data); i++ {
		shift := uint(i) * 8
		clearMask := byte(^((mask << uint(bitOff)) >> shift))
		data[byteOff+i] &= clearMask
		data[byteOff+i] |= byte((val << uint(bitOff)) >> shift)
	}
}
