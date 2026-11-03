package signal

import (
	"bytes"
	"strings"
	"testing"
)

func TestNDEFRoundTrip(t *testing.T) {
	recs := []NDEFRecord{{Kind: "text", Lang: "en", Text: "unlock the door cw-1234abcd"}, {Kind: "uri", URI: "https://www.example.com/a"}, {Kind: "uri", URI: "tel:+15551234"}, {Kind: "uri", URI: "ftp://x/y"}}
	b, err := NDEF(recs)
	if err != nil {
		t.Fatal(err)
	}
	if b[0]&0xC0 != 0x80 {
		t.Errorf("first record must have MB and not ME: %#x", b[0])
	}
	got, err := NDEFDecode(b)
	if err != nil || len(got) != len(recs) {
		t.Fatalf("decode: %v %+v", err, got)
	}
	for i := range recs {
		if recs[i].Kind == "text" && (got[i].Text != recs[i].Text || got[i].Lang != "en") || recs[i].Kind == "uri" && got[i].URI != recs[i].URI {
			t.Errorf("record %d: %+v != %+v", i, got[i], recs[i])
		}
	}
	// The https://www. prefix is abbreviated to one byte on the wire.
	if bytes.Contains(b, []byte("https://www.")) {
		t.Error("URI prefix should be abbreviated")
	}
	if !strings.Contains(NDEFText(got), "unlock the door") {
		t.Error("NDEFText")
	}
}

func TestNDEFLongRecordAndErrors(t *testing.T) {
	long := strings.Repeat("a", 300)
	b, err := NDEF([]NDEFRecord{{Kind: "text", Text: long}})
	if err != nil || b[0]&0x10 != 0 {
		t.Fatalf("a long record must not use the short form: %v %#x", err, b[0])
	}
	got, err := NDEFDecode(b)
	if err != nil || got[0].Text != long {
		t.Fatalf("long round trip: %v", err)
	}
	if _, err := NDEF(nil); err == nil {
		t.Error("empty message")
	}
	if _, err := NDEF([]NDEFRecord{{Kind: "smart-poster"}}); err == nil {
		t.Error("unknown kind")
	}
	for _, bad := range [][]byte{{}, {0xD1}, {0xD1, 1, 9, 'T', 1}, {0xD2, 1, 1, 'X', 0}} {
		if _, err := NDEFDecode(bad); err == nil {
			t.Errorf("%v should not decode", bad)
		}
	}
	if !strings.Contains(NDEFCapacity(100), "NTAG213") || !strings.Contains(NDEFCapacity(500), "NTAG215") || !strings.Contains(NDEFCapacity(2000), "larger") {
		t.Error("capacity text")
	}
}

func TestBLEAdvertising(t *testing.T) {
	b, err := BLEAdvertising(BLEAdvert{Name: "Door Lock cw-1a2b3c4d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > BLELegacyMax || b[0] != 2 || b[1] != adFlags {
		t.Fatalf("legacy adv: % x", b)
	}
	f, err := BLEDecode(b)
	if err != nil || f.Name != "Door Lock cw-1a2b3c4d" || f.Flags != 0x06 {
		t.Fatalf("decode: %v %+v", err, f)
	}
	_, err = BLEAdvertising(BLEAdvert{Name: strings.Repeat("n", 27)})
	if err == nil || !strings.Contains(err.Error(), "31") || !strings.Contains(err.Error(), "extended") {
		t.Errorf("27-byte name overflows a legacy PDU: %v", err)
	}
	b, err = BLEAdvertising(BLEAdvert{Name: "unlock", Manufacturer: strings.Repeat("m", 100), Extended: true})
	if err != nil {
		t.Fatal(err)
	}
	f, err = BLEDecode(b)
	if err != nil || len(f.Manufacturer) != 100 || f.CompanyID != 0xFFFF || !strings.HasPrefix(f.Text(), "unlock m") {
		t.Fatalf("extended: %v %+v", err, f)
	}
	if _, err := BLEAdvertising(BLEAdvert{Extended: true}); err == nil {
		t.Error("empty advert")
	}
	if _, err := BLEAdvertising(BLEAdvert{Name: strings.Repeat("x", 300), Extended: true}); err == nil {
		t.Error("over 254 must fail")
	}
	if _, err := BLEDecode([]byte{9, 9, 1}); err == nil {
		t.Error("truncated AD must fail")
	}
}

func TestWAVInfo(t *testing.T) {
	wav := WAV(16000, make([]int16, 8000))
	f, err := WAVInfo(wav)
	if err != nil || f.Rate != 16000 || f.Channels != 1 || f.Bits != 16 || f.Seconds < 0.49 || f.Seconds > 0.51 {
		t.Fatalf("%v %+v", err, f)
	}
	if !strings.Contains(f.String(), "16-bit mono 16000 Hz") {
		t.Error(f.String())
	}
	for _, bad := range [][]byte{nil, []byte("RIFFxxxxWAVE"), append([]byte("RIFF\x00\x00\x00\x00WAVEjunk"), make([]byte, 40)...)} {
		if _, err := WAVInfo(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestBitPulses(t *testing.T) {
	p, err := BitPulses("1101 0001")
	if err != nil {
		t.Fatal(err)
	}
	if PulseBits(p) != "11010001" {
		t.Errorf("round trip: %s", PulseBits(p))
	}
	if BlinkUnitsJSON(p) != `[["on",2],["off",1],["on",1],["off",3],["on",1]]` {
		t.Errorf("json: %s", BlinkUnitsJSON(p))
	}
	long, _ := BitPulses(strings.Repeat("1", 45))
	if len(long) != 3 || long[0].Units != 20 || long[2].Units != 5 || PulseBits(long) != strings.Repeat("1", 45) {
		t.Errorf("runs split at 20: %+v", long)
	}
	if _, err := BitPulses("10x1"); err == nil {
		t.Error("non-bit")
	}
	if _, err := BitPulses("  "); err == nil {
		t.Error("empty")
	}
}
