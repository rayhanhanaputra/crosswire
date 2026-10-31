package signal

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
)

// DTMFDecode recovers the key sequence from 16-bit mono PCM WAV audio with
// a Goertzel filter bank, the same way a telephony front end does. It is
// used by the reference voice target so the thing under test really decodes
// tones; the Workbench uses it to prove a rendered clip still says what the
// operator typed.
func DTMFDecode(wav []byte) (string, error) {
	rate, samples, err := parseWAV(wav)
	if err != nil {
		return "", err
	}
	frame := rate * 20 / 1000 // 20 ms frames
	if frame == 0 || len(samples) < frame {
		return "", errors.New("clip too short")
	}
	rows := []float64{697, 770, 852, 941}
	cols := []float64{1209, 1336, 1477, 1633}
	keys := [4][4]byte{
		{'1', '2', '3', 'A'},
		{'4', '5', '6', 'B'},
		{'7', '8', '9', 'C'},
		{'*', '0', '#', 'D'},
	}
	var out strings.Builder
	var last, cand byte
	run := 0
	silent := 0
	for off := 0; off+frame <= len(samples); off += frame {
		seg := samples[off : off+frame]
		energy := 0.0
		for _, s := range seg {
			energy += float64(s) * float64(s)
		}
		if energy/float64(frame) < 1e5 { // silence threshold for 16-bit PCM
			silent++
			if silent >= 2 {
				last = 0
			}
			continue
		}
		silent = 0
		ri, rp, rtotal := bestBin(seg, rate, rows)
		ci, cp, ctotal := bestBin(seg, rate, cols)
		// Both tones must be present, each the clear winner of its group,
		// and together they must carry most of the frame's energy. Without
		// the last two checks, speech trips the detector.
		tonal := rp >= 1e9 && cp >= 1e9 && rp >= 2*(rtotal-rp) && cp >= 2*(ctotal-cp) &&
			(rp+cp)/(float64(frame)*energy) >= 0.35
		if !tonal {
			cand, run = 0, 0
			continue
		}
		key := keys[ri][ci]
		if key == cand {
			run++
		} else {
			cand, run = key, 1
		}
		if run >= 2 && key != last { // a key must hold for two frames (40 ms)
			out.WriteByte(key)
			last = key
		}
	}
	if out.Len() == 0 {
		return "", errors.New("no DTMF tones detected")
	}
	return out.String(), nil
}

func bestBin(seg []int16, rate int, freqs []float64) (int, float64, float64) {
	best, bestPower, total := 0, -1.0, 0.0
	for i, f := range freqs {
		p := goertzel(seg, rate, f)
		total += p
		if p > bestPower {
			best, bestPower = i, p
		}
	}
	return best, bestPower, total
}

func goertzel(seg []int16, rate int, freq float64) float64 {
	coeff := 2 * math.Cos(2*math.Pi*freq/float64(rate))
	var s0, s1, s2 float64
	for _, s := range seg {
		s0 = float64(s) + coeff*s1 - s2
		s2, s1 = s1, s0
	}
	return s1*s1 + s2*s2 - coeff*s1*s2
}

func parseWAV(wav []byte) (int, []int16, error) {
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return 0, nil, errors.New("not a RIFF/WAVE file")
	}
	rate := 0
	pos := 12
	for pos+8 <= len(wav) {
		id := string(wav[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(wav[pos+4 : pos+8]))
		body := wav[pos+8:]
		if size > len(body) {
			size = len(body)
		}
		switch id {
		case "fmt ":
			if size < 16 || binary.LittleEndian.Uint16(body[0:2]) != 1 || binary.LittleEndian.Uint16(body[2:4]) != 1 || binary.LittleEndian.Uint16(body[14:16]) != 16 {
				return 0, nil, errors.New("only 16-bit mono PCM is supported")
			}
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
		case "data":
			if rate == 0 {
				return 0, nil, errors.New("data chunk before fmt chunk")
			}
			samples := make([]int16, size/2)
			for i := range samples {
				samples[i] = int16(binary.LittleEndian.Uint16(body[2*i : 2*i+2]))
			}
			return rate, samples, nil
		}
		pos += 8 + size + size%2
	}
	return 0, nil, errors.New("no data chunk")
}
