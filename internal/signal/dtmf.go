package signal

import (
	"fmt"
	"strings"
)

// ITU-T Q.23 DTMF frequency pairs.
var dtmfRows = map[byte]float64{
	'1': 697, '2': 697, '3': 697, 'A': 697,
	'4': 770, '5': 770, '6': 770, 'B': 770,
	'7': 852, '8': 852, '9': 852, 'C': 852,
	'*': 941, '0': 941, '#': 941, 'D': 941,
}

var dtmfCols = map[byte]float64{
	'1': 1209, '4': 1209, '7': 1209, '*': 1209,
	'2': 1336, '5': 1336, '8': 1336, '0': 1336,
	'3': 1477, '6': 1477, '9': 1477, '#': 1477,
	'A': 1633, 'B': 1633, 'C': 1633, 'D': 1633,
}

// DTMFWAV renders a key sequence as audio. toneMs and gapMs default to 100,
// which is well above the 40 ms minimum every decoder accepts. Characters
// outside 0-9, *, #, A-D are rejected rather than skipped so a typo cannot
// silently shorten the sequence.
func DTMFWAV(keys string, toneMs, gapMs int) ([]byte, error) {
	if toneMs <= 0 {
		toneMs = 100
	}
	if gapMs <= 0 {
		gapMs = 100
	}
	const rate = 8000
	keys = strings.ToUpper(strings.ReplaceAll(keys, " ", ""))
	if keys == "" {
		return nil, fmt.Errorf("empty DTMF sequence")
	}
	var samples []int16
	for i := 0; i < len(keys); i++ {
		row, ok1 := dtmfRows[keys[i]]
		col, ok2 := dtmfCols[keys[i]]
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%q is not a DTMF key", keys[i])
		}
		samples = append(samples, Tone(rate, rate*toneMs/1000, 0.8, row, col)...)
		samples = append(samples, make([]int16, rate*gapMs/1000)...)
	}
	return WAV(rate, samples), nil
}
