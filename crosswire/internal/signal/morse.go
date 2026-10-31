// Package signal renders attack payloads as real physical artifacts: Morse
// pulse timelines, QR images, DTMF audio, and text rendered into images.
//
// Every encoder here produces something that can be put in front of a real
// sensor. If an encoder only renames a JSON key, it does not belong in this
// package.
package signal

import (
	"fmt"
	"strings"
)

// Pulse is one step of an on/off timeline measured in Morse units.
type Pulse struct {
	On    bool `json:"on"`
	Units int  `json:"units"`
}

var morseTable = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.",
	'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
	'.': ".-.-.-", ',': "--..--", '?': "..--..", '/': "-..-.", '-': "-....-",
	'=': "-...-", ':': "---...", '@': ".--.-.",
}

var morseInverse = func() map[string]rune {
	m := make(map[string]rune, len(morseTable))
	for r, code := range morseTable {
		m[code] = r
	}
	return m
}()

// MorseEncode renders text as ITU Morse. Letters are separated by a space,
// words by " / ". Characters without a Morse code are dropped.
func MorseEncode(text string) string {
	var words []string
	for _, word := range strings.Fields(strings.ToUpper(text)) {
		var letters []string
		for _, r := range word {
			if code, ok := morseTable[r]; ok {
				letters = append(letters, code)
			}
		}
		if len(letters) > 0 {
			words = append(words, strings.Join(letters, " "))
		}
	}
	return strings.Join(words, " / ")
}

// MorseDecode turns a Morse string back into text. Unknown symbols become '?'.
func MorseDecode(morse string) string {
	var out []string
	for _, word := range strings.Split(morse, " / ") {
		var b strings.Builder
		for _, sym := range strings.Fields(word) {
			if r, ok := morseInverse[sym]; ok {
				b.WriteRune(r)
			} else {
				b.WriteByte('?')
			}
		}
		out = append(out, b.String())
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

// MorsePulses converts a Morse string into an ITU-timed on/off timeline:
// dot 1, dash 3, gap inside a letter 1, gap between letters 3, between words 7.
// Consecutive off steps are merged.
func MorsePulses(morse string) []Pulse {
	var out []Pulse
	push := func(on bool, units int) {
		if n := len(out); n > 0 && !on && !out[n-1].On {
			out[n-1].Units += units
			return
		}
		out = append(out, Pulse{On: on, Units: units})
	}
	words := strings.Split(morse, " / ")
	for wi, word := range words {
		if wi > 0 {
			push(false, 7)
		}
		letters := strings.Fields(word)
		for li, letter := range letters {
			if li > 0 {
				push(false, 3)
			}
			for si, sym := range letter {
				if si > 0 {
					push(false, 1)
				}
				switch sym {
				case '.':
					push(true, 1)
				case '-':
					push(true, 3)
				}
			}
		}
	}
	return out
}

// PulsesDecode is the inverse of MorsePulses with tolerance: on-steps < 2
// units are dots, longer are dashes; off-steps < 2 are intra-letter, < 5 are
// letter gaps, otherwise word gaps. It exists so a run can prove the timeline
// it sent still decodes to the intended text.
func PulsesDecode(pulses []Pulse) string {
	var b strings.Builder
	for _, p := range pulses {
		switch {
		case p.On && p.Units < 2:
			b.WriteByte('.')
		case p.On:
			b.WriteByte('-')
		case p.Units < 2:
			// intra-letter gap
		case p.Units < 5:
			b.WriteByte(' ')
		default:
			b.WriteString(" / ")
		}
	}
	return MorseDecode(b.String())
}

// PulsesDuration returns the total length of a timeline for a given unit length.
func PulsesDuration(pulses []Pulse, unitMs int) int {
	total := 0
	for _, p := range pulses {
		total += p.Units * unitMs
	}
	return total
}

// WPM returns the unit length in milliseconds for a words-per-minute speed
// using the PARIS standard (50 units per word).
func WPM(wpm int) int {
	if wpm <= 0 {
		wpm = 12
	}
	return 1200 / wpm
}

// MorseWAV renders a pulse timeline as a sidetone audio clip.
func MorseWAV(pulses []Pulse, unitMs int, toneHz float64) []byte {
	if unitMs <= 0 {
		unitMs = 100
	}
	if toneHz <= 0 {
		toneHz = 700
	}
	const rate = 16000
	var samples []int16
	for _, p := range pulses {
		n := rate * p.Units * unitMs / 1000
		if p.On {
			samples = append(samples, Tone(rate, n, 0.7, toneHz)...)
		} else {
			samples = append(samples, make([]int16, n)...)
		}
	}
	return WAV(rate, samples)
}

// LEDSequenceCommand builds the JSON command understood by the repository's
// Arduino/ESP32 LED rig (hardware/arduino/aat_led_rig). The firmware refuses
// units > 20 per step, unit_ms outside 50..2000 and more than maxSteps steps,
// so those limits are enforced here first and reported as an error rather
// than failing on the wire.
func LEDSequenceCommand(pulses []Pulse, pin, unitMs, watchdogMs, maxSteps int) (map[string]any, error) {
	if unitMs < 50 || unitMs > 2000 {
		return nil, fmt.Errorf("unit_ms %d outside the rig's 50..2000 range", unitMs)
	}
	if len(pulses) == 0 {
		return nil, fmt.Errorf("empty pulse timeline")
	}
	if maxSteps > 0 && len(pulses) > maxSteps {
		return nil, fmt.Errorf("timeline has %d steps; rig accepts at most %d", len(pulses), maxSteps)
	}
	if need := PulsesDuration(pulses, unitMs) + unitMs; watchdogMs < need {
		return nil, fmt.Errorf("watchdog_ms %d is shorter than the %d ms timeline", watchdogMs, need)
	}
	steps := make([]map[string]any, 0, len(pulses))
	for _, p := range pulses {
		if p.Units < 1 || p.Units > 20 {
			return nil, fmt.Errorf("step of %d units outside the rig's 1..20 range", p.Units)
		}
		state := "off"
		if p.On {
			state = "on"
		}
		steps = append(steps, map[string]any{"state": state, "units": p.Units})
	}
	return map[string]any{
		"command":     "led_sequence",
		"pin":         pin,
		"unit_ms":     unitMs,
		"watchdog_ms": watchdogMs,
		"steps":       steps,
	}, nil
}

// LEDStopCommand is the rig's mandatory stop command.
func LEDStopCommand(pin int) map[string]any {
	return map[string]any{"command": "stop", "pin": pin}
}
