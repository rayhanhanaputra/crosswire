package signal

import (
	"math"
	"testing"
)

func TestMuLawRoundTrip(t *testing.T) {
	for _, s := range []int16{0, 1, -1, 100, -100, 1000, -1000, 8000, -8000, 32000, -32000, 32767, -32768} {
		back := MuLawDecode(MuLawEncode(s))
		// μ-law is logarithmic: error grows with magnitude, about 3 % of it.
		if tol := math.Abs(float64(s))*0.04 + 8; math.Abs(float64(back)-float64(s)) > tol {
			t.Fatalf("%d -> %d (tolerance %.0f)", s, back, tol)
		}
	}
}

func TestWAVParseAndResample(t *testing.T) {
	tone := Tone(24000, 24000, 0.5, 440) // 1 s at 24 kHz
	wav := WAV(24000, tone)
	rate, pcm, err := ParseWAV(wav)
	if err != nil || rate != 24000 || len(pcm) != 24000 {
		t.Fatalf("%d %d %v", rate, len(pcm), err)
	}
	ulaw, err := ToMuLaw8k(wav)
	if err != nil || len(ulaw) != 8000 {
		t.Fatalf("%d %v", len(ulaw), err)
	}
	back := FromMuLaw8k(ulaw)
	r2, pcm2, err := ParseWAV(back)
	if err != nil || r2 != 8000 || len(pcm2) != 8000 {
		t.Fatalf("%d %d %v", r2, len(pcm2), err)
	}
	// A 440 Hz tone keeps its zero crossings through resample and codec.
	zc := 0
	for i := 1; i < len(pcm2); i++ {
		if (pcm2[i-1] < 0) != (pcm2[i] < 0) {
			zc++
		}
	}
	if zc < 860 || zc > 900 {
		t.Fatalf("zero crossings %d, want about 880", zc)
	}
	if _, _, err := ParseWAV([]byte("nope")); err == nil {
		t.Fatal("non-WAV must fail")
	}
}
