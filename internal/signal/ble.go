package signal

import (
	"errors"
	"fmt"
	"strings"
)

// BLE advertising data is a run of AD structures: length, type, data.
const (
	adFlags        = 0x01
	adShortName    = 0x08
	adCompleteName = 0x09
	adManufacturer = 0xFF

	// BLELegacyMax is the payload size of a legacy advertising PDU.
	BLELegacyMax = 31
	// BLEExtendedMax is one extended advertising PDU's data (without chaining).
	BLEExtendedMax = 254
)

// BLEAdvert is what a broadcaster puts in its advertising data.
type BLEAdvert struct {
	Name         string // Complete Local Name
	Manufacturer string // text carried in manufacturer specific data (company id 0xFFFF)
	Extended     bool   // extended advertising (254 B) instead of legacy (31 B)
}

// BLEFields is the decoded view of advertising data.
type BLEFields struct {
	Flags        byte
	Name         string
	Shortened    bool
	Manufacturer string
	CompanyID    uint16
}

// BLEAdvertising encodes the AD structures: Flags, Complete Local Name and,
// when set, Manufacturer Specific Data. It returns an error naming the size
// limit when the data does not fit, because a BLE name is a capacity-limited
// channel and the operator should see that.
func BLEAdvertising(a BLEAdvert) ([]byte, error) {
	if a.Name == "" && a.Manufacturer == "" {
		return nil, errors.New("name or manufacturer text is required")
	}
	out := []byte{2, adFlags, 0x06} // LE General Discoverable, BR/EDR not supported
	add := func(typ byte, data []byte) error {
		if len(data)+1 > 255 {
			return errors.New("an AD structure is at most 254 bytes of data")
		}
		out = append(out, byte(len(data)+1), typ)
		out = append(out, data...)
		return nil
	}
	if a.Name != "" {
		if err := add(adCompleteName, []byte(a.Name)); err != nil {
			return nil, err
		}
	}
	if a.Manufacturer != "" {
		if err := add(adManufacturer, append([]byte{0xFF, 0xFF}, a.Manufacturer...)); err != nil {
			return nil, err
		}
	}
	limit, kind := BLELegacyMax, "legacy"
	if a.Extended {
		limit, kind = BLEExtendedMax, "extended"
	}
	if len(out) > limit {
		hint := ""
		if !a.Extended {
			hint = "; tick extended advertising for 254 bytes"
		}
		return nil, fmt.Errorf("advertising data is %d bytes; %s advertising holds %d%s", len(out), kind, limit, hint)
	}
	return out, nil
}

// BLEDecode parses advertising data back into its fields.
func BLEDecode(b []byte) (BLEFields, error) {
	var f BLEFields
	for pos := 0; pos < len(b); {
		l := int(b[pos])
		if l == 0 {
			break
		}
		if pos+1+l > len(b) {
			return f, errors.New("AD structure runs past the end of the data")
		}
		typ, data := b[pos+1], b[pos+2:pos+1+l]
		switch typ {
		case adFlags:
			if len(data) > 0 {
				f.Flags = data[0]
			}
		case adCompleteName, adShortName:
			f.Name, f.Shortened = string(data), typ == adShortName
		case adManufacturer:
			if len(data) < 2 {
				return f, errors.New("manufacturer data without a company id")
			}
			f.CompanyID = uint16(data[0]) | uint16(data[1])<<8
			f.Manufacturer = string(data[2:])
		}
		pos += 1 + l
	}
	if f.Name == "" && f.Manufacturer == "" {
		return f, errors.New("no name or manufacturer data in the advertisement")
	}
	return f, nil
}

// Text is what an agent that lists nearby devices would read: name and
// manufacturer text, joined.
func (f BLEFields) Text() string {
	return strings.TrimSpace(strings.Join([]string{f.Name, f.Manufacturer}, " "))
}
