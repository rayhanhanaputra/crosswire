package signal

import (
	"encoding/json"
	"errors"
	"strings"
)

// BitPulses turns a bit string into an on/off timeline of one unit per bit.
// Equal neighbours merge, and a run is split at 20 units because the LED rig
// refuses longer steps. Spaces, commas and underscores in the input are ignored.
func BitPulses(bits string) ([]Pulse, error) {
	var out []Pulse
	for _, r := range bits {
		var on bool
		switch r {
		case '1':
			on = true
		case '0':
		case ' ', ',', '_', '\t', '\n':
			continue
		default:
			return nil, errors.New("bits may contain only 0 and 1")
		}
		if n := len(out); n > 0 && out[n-1].On == on && out[n-1].Units < 20 {
			out[n-1].Units++
			continue
		}
		out = append(out, Pulse{On: on, Units: 1})
	}
	if len(out) == 0 {
		return nil, errors.New("bits is required")
	}
	return out, nil
}

// PulseBits is the inverse of BitPulses for a one-unit-per-bit timeline.
func PulseBits(pulses []Pulse) string {
	var b strings.Builder
	for _, p := range pulses {
		c := "0"
		if p.On {
			c = "1"
		}
		b.WriteString(strings.Repeat(c, p.Units))
	}
	return b.String()
}

// BlinkUnitsJSON renders a timeline as [["on",1],["off",3],...], the form the
// bridge protocol and the Python workbench's blink_units use.
func BlinkUnitsJSON(pulses []Pulse) string {
	steps := make([][2]any, 0, len(pulses))
	for _, p := range pulses {
		s := "off"
		if p.On {
			s = "on"
		}
		steps = append(steps, [2]any{s, p.Units})
	}
	b, _ := json.Marshal(steps)
	return string(b)
}
