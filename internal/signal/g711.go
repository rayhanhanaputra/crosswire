package signal

import (
	"encoding/binary"
	"fmt"
)

// Telephony audio: WAV in, 8 kHz G.711 μ-law out and back. This is the
// codec every SIP trunk and PBX accepts without negotiation.

// ParseWAV reads 16-bit PCM WAV (mono or stereo, mixed to mono).
func ParseWAV(b []byte) (rate int, samples []int16, err error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, nil, fmt.Errorf("not a RIFF/WAVE file")
	}
	var channels, bits int
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := b[pos+8:]
		if size > len(body) {
			size = len(body)
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return 0, nil, fmt.Errorf("short fmt chunk")
			}
			if f := binary.LittleEndian.Uint16(body[0:2]); f != 1 && f != 0xFFFE {
				return 0, nil, fmt.Errorf("WAV format %d is not PCM", f)
			}
			channels = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits = int(binary.LittleEndian.Uint16(body[14:16]))
		case "data":
			if rate == 0 || channels == 0 {
				return 0, nil, fmt.Errorf("data chunk before fmt chunk")
			}
			if bits != 16 {
				return 0, nil, fmt.Errorf("%d-bit WAV is not supported; use 16-bit PCM", bits)
			}
			frame := 2 * channels
			n := len(body) / frame
			samples = make([]int16, n)
			for i := 0; i < n; i++ {
				var acc int
				for c := 0; c < channels; c++ {
					acc += int(int16(binary.LittleEndian.Uint16(body[i*frame+c*2:])))
				}
				samples[i] = int16(acc / channels)
			}
			return rate, samples, nil
		}
		pos += 8 + size + size%2
	}
	return 0, nil, fmt.Errorf("no data chunk")
}

// Resample converts PCM between sample rates by linear interpolation.
// Telephone-band speech loses nothing audible to an ASR at 8 kHz.
func Resample(in []int16, from, to int) []int16 {
	if from == to || len(in) == 0 || from <= 0 || to <= 0 {
		return in
	}
	n := int(int64(len(in)) * int64(to) / int64(from))
	out := make([]int16, n)
	for i := range out {
		pos := float64(i) * float64(from) / float64(to)
		j := int(pos)
		if j+1 >= len(in) {
			out[i] = in[len(in)-1]
			continue
		}
		frac := pos - float64(j)
		out[i] = int16(float64(in[j])*(1-frac) + float64(in[j+1])*frac)
	}
	return out
}

const muBias = 0x84

// MuLawEncode converts one 16-bit sample to G.711 μ-law.
func MuLawEncode(s int16) byte {
	v := int(s)
	sign := byte(0)
	if v < 0 {
		v = -v
		sign = 0x80
	}
	if v > 32635 {
		v = 32635
	}
	v += muBias
	exp := 7
	for mask := 0x4000; exp > 0 && v&mask == 0; mask >>= 1 {
		exp--
	}
	mant := (v >> (exp + 3)) & 0x0F
	return ^(sign | byte(exp<<4) | byte(mant))
}

// MuLawDecode converts one G.711 μ-law byte to a 16-bit sample.
func MuLawDecode(b byte) int16 {
	b = ^b
	sign := b & 0x80
	exp := int(b>>4) & 0x07
	mant := int(b & 0x0F)
	v := ((mant << 3) + muBias) << exp
	v -= muBias
	if sign != 0 {
		return int16(-v)
	}
	return int16(v)
}

// ToMuLaw8k turns any 16-bit WAV into an 8 kHz μ-law byte stream.
func ToMuLaw8k(wav []byte) ([]byte, error) {
	rate, pcm, err := ParseWAV(wav)
	if err != nil {
		return nil, err
	}
	pcm = Resample(pcm, rate, 8000)
	out := make([]byte, len(pcm))
	for i, s := range pcm {
		out[i] = MuLawEncode(s)
	}
	return out, nil
}

// FromMuLaw8k turns a μ-law stream back into a 16-bit 8 kHz WAV.
func FromMuLaw8k(ulaw []byte) []byte {
	pcm := make([]int16, len(ulaw))
	for i, b := range ulaw {
		pcm[i] = MuLawDecode(b)
	}
	return WAV(8000, pcm)
}
