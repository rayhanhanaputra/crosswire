package signal

import (
	"bytes"
	"encoding/binary"
	"math"
)

// Tone synthesizes n samples of a sine wave with a 5 ms raised-cosine ramp at
// each end so the clip has no clicks that a VAD or ASR front end would flag.
func Tone(rate, n int, amplitude float64, freqs ...float64) []int16 {
	out := make([]int16, n)
	if n == 0 || len(freqs) == 0 {
		return out
	}
	ramp := rate * 5 / 1000
	if ramp*2 > n {
		ramp = n / 2
	}
	per := amplitude / float64(len(freqs))
	for i := 0; i < n; i++ {
		t := float64(i) / float64(rate)
		v := 0.0
		for _, f := range freqs {
			v += per * math.Sin(2*math.Pi*f*t)
		}
		env := 1.0
		if i < ramp {
			env = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(ramp))
		} else if i >= n-ramp {
			env = 0.5 - 0.5*math.Cos(math.Pi*float64(n-1-i)/float64(ramp))
		}
		out[i] = int16(v * env * 32767)
	}
	return out
}

// WAV wraps 16-bit mono PCM samples in a RIFF/WAVE container.
func WAV(rate int, samples []int16) []byte {
	var data bytes.Buffer
	for _, s := range samples {
		_ = binary.Write(&data, binary.LittleEndian, s)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+data.Len()))
	out.WriteString("WAVEfmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // mono
	_ = binary.Write(&out, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&out, binary.LittleEndian, uint32(rate*2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(data.Len()))
	out.Write(data.Bytes())
	return out.Bytes()
}
