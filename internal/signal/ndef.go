package signal

import (
	"errors"
	"fmt"
	"strings"
)

// NDEFRecord is one record of an NFC Forum NDEF message. Kind is "text"
// (well-known type T) or "uri" (well-known type U).
type NDEFRecord struct {
	Kind string
	Lang string // text records; default "en"
	Text string // text records
	URI  string // uri records
}

// uriPrefixes are the NFC Forum URI Record Type abbreviations that matter for
// the schemes an operator is likely to type.
var uriPrefixes = []string{"", "http://www.", "https://www.", "http://", "https://", "tel:", "mailto:"}

// NDEF encodes records as one NDEF message: MB on the first record, ME on the
// last, SR (short record) whenever the payload is under 256 bytes.
func NDEF(records []NDEFRecord) ([]byte, error) {
	if len(records) == 0 {
		return nil, errors.New("an NDEF message needs at least one record")
	}
	var out []byte
	for i, r := range records {
		var typ byte
		var payload []byte
		switch r.Kind {
		case "text":
			lang := r.Lang
			if lang == "" {
				lang = "en"
			}
			if len(lang) > 63 {
				return nil, errors.New("language code is longer than 63 bytes")
			}
			typ = 'T'
			payload = append([]byte{byte(len(lang))}, lang...) // UTF-8, status bit 7 clear
			payload = append(payload, r.Text...)
		case "uri":
			typ = 'U'
			code := 0
			uri := r.URI
			for c := len(uriPrefixes) - 1; c >= 1; c-- {
				if strings.HasPrefix(strings.ToLower(uri), uriPrefixes[c]) {
					code, uri = c, uri[len(uriPrefixes[c]):]
					break
				}
			}
			payload = append([]byte{byte(code)}, uri...)
		default:
			return nil, fmt.Errorf("unknown NDEF record kind %q", r.Kind)
		}
		hdr := byte(0x01) // TNF well-known
		if i == 0 {
			hdr |= 0x80
		}
		if i == len(records)-1 {
			hdr |= 0x40
		}
		if len(payload) < 256 {
			hdr |= 0x10
			out = append(out, hdr, 1, byte(len(payload)), typ)
		} else {
			out = append(out, hdr, 1, byte(len(payload)>>24), byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)), typ)
		}
		out = append(out, payload...)
	}
	return out, nil
}

// NDEFDecode parses a message produced by NDEF (and any well-known text or URI
// records from a real tag). It is the round-trip check.
func NDEFDecode(b []byte) ([]NDEFRecord, error) {
	var recs []NDEFRecord
	pos := 0
	for pos < len(b) {
		if pos+3 > len(b) {
			return nil, errors.New("truncated NDEF record header")
		}
		hdr := b[pos]
		if hdr&0x07 != 0x01 {
			return nil, fmt.Errorf("unsupported TNF %d", hdr&0x07)
		}
		if hdr&0x20 != 0 {
			return nil, errors.New("chunked NDEF records are not supported")
		}
		typeLen := int(b[pos+1])
		pos += 2
		var plen int
		if hdr&0x10 != 0 {
			plen = int(b[pos])
			pos++
		} else {
			if pos+4 > len(b) {
				return nil, errors.New("truncated NDEF payload length")
			}
			plen = int(b[pos])<<24 | int(b[pos+1])<<16 | int(b[pos+2])<<8 | int(b[pos+3])
			pos += 4
		}
		if hdr&0x08 != 0 { // ID length present
			pos++
		}
		if pos+typeLen+plen > len(b) || plen < 0 {
			return nil, errors.New("NDEF record runs past the end of the message")
		}
		typ := string(b[pos : pos+typeLen])
		payload := b[pos+typeLen : pos+typeLen+plen]
		pos += typeLen + plen
		switch typ {
		case "T":
			if len(payload) < 1 {
				return nil, errors.New("empty text record")
			}
			ll := int(payload[0] & 0x3F)
			if payload[0]&0x80 != 0 {
				return nil, errors.New("UTF-16 text records are not supported")
			}
			if 1+ll > len(payload) {
				return nil, errors.New("text record language runs past the payload")
			}
			recs = append(recs, NDEFRecord{Kind: "text", Lang: string(payload[1 : 1+ll]), Text: string(payload[1+ll:])})
		case "U":
			if len(payload) < 1 || int(payload[0]) >= len(uriPrefixes) {
				return nil, errors.New("bad URI record")
			}
			recs = append(recs, NDEFRecord{Kind: "uri", URI: uriPrefixes[payload[0]] + string(payload[1:])})
		default:
			return nil, fmt.Errorf("unsupported NDEF type %q", typ)
		}
		if hdr&0x40 != 0 {
			break
		}
	}
	if len(recs) == 0 {
		return nil, errors.New("no NDEF records")
	}
	return recs, nil
}

// NDEFText joins the decoded records into the text an NFC reader app passes on.
func NDEFText(recs []NDEFRecord) string {
	var parts []string
	for _, r := range recs {
		if r.Kind == "uri" {
			parts = append(parts, r.URI)
		} else {
			parts = append(parts, r.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// NDEFCapacity says which common tag types can hold n bytes of NDEF data.
// The figures are the usable user-memory sizes of NTAG213, NTAG215 and NTAG216.
func NDEFCapacity(n int) string {
	switch {
	case n <= 144:
		return "fits NTAG213 (144 B), NTAG215 and NTAG216"
	case n <= 504:
		return "needs NTAG215 (504 B) or NTAG216"
	case n <= 888:
		return "needs NTAG216 (888 B)"
	}
	return "larger than the common NTAG tags (888 B)"
}
