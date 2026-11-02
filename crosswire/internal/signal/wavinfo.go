package signal

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// WAVFormat describes a WAV file.
type WAVFormat struct {
	Format   int // 1 PCM, 3 IEEE float
	Channels int
	Rate     int
	Bits     int
	Seconds  float64
}

// WAVInfo validates the RIFF/WAVE container and returns its format and length.
// It accepts PCM and float WAVs of any channel count; it does not decode them.
func WAVInfo(wav []byte) (WAVFormat, error) {
	var f WAVFormat
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return f, errors.New("not a RIFF/WAVE file")
	}
	dataBytes, haveFmt, haveData := 0, false, false
	for pos := 12; pos+8 <= len(wav); {
		id := string(wav[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(wav[pos+4 : pos+8]))
		body := wav[pos+8:]
		if size > len(body) {
			size = len(body)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return f, errors.New("fmt chunk is too short")
			}
			f.Format = int(binary.LittleEndian.Uint16(body[0:2]))
			f.Channels = int(binary.LittleEndian.Uint16(body[2:4]))
			f.Rate = int(binary.LittleEndian.Uint32(body[4:8]))
			f.Bits = int(binary.LittleEndian.Uint16(body[14:16]))
			haveFmt = true
		case "data":
			dataBytes, haveData = size, true
		}
		pos += 8 + size + size%2
	}
	switch {
	case !haveFmt:
		return f, errors.New("no fmt chunk")
	case !haveData:
		return f, errors.New("no data chunk")
	case f.Format != 1 && f.Format != 3:
		return f, fmt.Errorf("WAV format %d is not PCM or float", f.Format)
	case f.Channels < 1 || f.Channels > 8 || f.Rate < 4000 || f.Rate > 192000 || f.Bits < 8 || f.Bits > 32:
		return f, errors.New("WAV format fields are out of range")
	}
	f.Seconds = float64(dataBytes) / float64(f.Rate*f.Channels*(f.Bits/8))
	return f, nil
}

// String is the one-line summary shown as the round trip of an uploaded clip.
func (f WAVFormat) String() string {
	ch := "mono"
	if f.Channels == 2 {
		ch = "stereo"
	} else if f.Channels > 2 {
		ch = fmt.Sprintf("%d channels", f.Channels)
	}
	return fmt.Sprintf("%d-bit %s %d Hz, %.2f s", f.Bits, ch, f.Rate, f.Seconds)
}
