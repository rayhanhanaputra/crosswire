package channel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rayhanhanaputra/crosswire/internal/connector"
	"github.com/rayhanhanaputra/crosswire/internal/model"
	"github.com/rayhanhanaputra/crosswire/internal/signal"
)

// The adapters below carry what the Python workbench's encoders carried. Each
// builds the artifact a real sender would produce where one exists (NDEF
// bytes, BLE advertising structures, a rendered image, a pulse timeline, a
// WAV) and decodes it back as a check. An adapter that only relabels text is
// marked modeled, and a modeled run cannot back a "reproduced" claim.

const maxUploadAudio = 6 << 20

var extraCatalogue = []Info{
	{"json-fields", "telemetry/json", "application/json", false, []string{"json"}, "A JSON telemetry message. The object you give is merged over sensor_id, unit and battery defaults. Use it for MQTT, WebSocket and HTTP targets that take structured fields.", nil},
	{"sensor-value", "sensor/value", "application/json", true, []string{"metric", "level", "unit", "note"}, "MODELED: one numeric reading as a JSON message. No sensor is driven. The value and the optional note are the attack.", nil},
	{"reading-stream", "sensor/stream", "application/json", true, []string{"window", "interval_ms"}, "MODELED: a window of readings as one JSON message, the way a control agent receives a batch. Give numbers separated by commas, or a JSON array.", nil},
	{"binary-toggle", "gpio/binary", "pulses", false, []string{"bits", "unit_ms"}, "Capacity demo: a bit string as an on/off timeline (a relay, a GPIO pin, an LED). The bridge can drive it; the sidetone WAV is the preview.", nil},
	{"ble-advert", "ble/advert", "application/x-ble-ad", false, []string{"name", "manufacturer", "extended"}, "Real BLE advertising data: Flags, Complete Local Name and optional manufacturer data, decoded back. Legacy advertising holds 31 bytes, extended 254. Sending it over the air needs a bridge target.", nil},
	{"nfc-tag", "nfc/ndef", "application/x-ndef", false, []string{"text", "lang", "uri"}, "Real NDEF message: a text record and an optional URI record, decoded back. Writing it to a tag needs a bridge target.", nil},
	{"ocr-text", "camera/ocr", "image/png", false, []string{"text", "contrast", "scale"}, "Text rendered into an image, like a printed label or a scanned page, for an OCR or vision step. Same renderer as sign, with an OCR-style source label.", nil},
	{"agent-note", "agent/note", "application/json", true, []string{"text"}, "MODELED: a note an upstream agent wrote into a shared record, sent as text. For a stored note that a later request reads, use handoff with a trigger endpoint.", nil},
	{"retrieved-doc", "rag/document", "application/json", true, []string{"title", "url", "score", "text"}, "MODELED: a snippet a retrieval step placed in the agent's context, sent as a JSON chunk. No vector store is involved.", nil},
	{"voice-transcript", "voice/transcript", "application/json", true, []string{"spoken", "confidence", "language"}, "MODELED: the text a speech recogniser would produce, delivered as text. No audio is made; use speech for a real clip.", nil},
	{"audio-upload", "microphone/audio", "audio/wav", false, []string{"audio_base64", "text", "filename"}, "A WAV you upload or record in the browser, sent as it is. Recorded audio is fixed: it cannot carry the per-trial nonce, so a target that requires the canary in tool arguments cannot be confirmed with it.", nil},
}

func init() { catalogue = append(catalogue, extraCatalogue...) }

// fieldKinds tells the UI how to edit each adapter's fields. A field that is
// not listed is a single-line text input.
var fieldKinds = map[string]map[string]string{
	"text":             {"text": "area"},
	"qr":               {"text": "area", "size": "number"},
	"sign":             {"text": "area", "contrast": "number", "scale": "number"},
	"invoice":          {"note": "area", "note_contrast": "number"},
	"speech":           {"text": "area", "audio_base64": "audio"},
	"dtmf":             {"tone_ms": "number", "gap_ms": "number"},
	"morse-led":        {"text": "area", "wpm": "number"},
	"handoff":          {"text": "area"},
	"pdf":              {"body": "area", "note": "area", "note_gray": "number"},
	"calendar":         {"description": "area"},
	"email":            {"body": "area"},
	"json-fields":      {"json": "json"},
	"sensor-value":     {"level": "number", "note": "area"},
	"reading-stream":   {"window": "area", "interval_ms": "number"},
	"binary-toggle":    {"bits": "text", "unit_ms": "number"},
	"ble-advert":       {"extended": "bool"},
	"nfc-tag":          {"text": "area"},
	"ocr-text":         {"text": "area", "contrast": "number", "scale": "number"},
	"agent-note":       {"text": "area"},
	"retrieved-doc":    {"text": "area", "score": "number"},
	"voice-transcript": {"spoken": "area", "confidence": "number"},
	"audio-upload":     {"audio_base64": "audio", "text": "area"},
}

func boolean(f map[string]any, k string) bool {
	switch v := f[k].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		}
	case float64:
		return v != 0
	}
	return false
}

// jsonArtifact packs a JSON message. content is the text delivered as
// {{content}}; the round trip is the compact JSON parsed and re-encoded.
func jsonArtifact(source, content string, doc any, modeled bool, summary string) (Encoded, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return Encoded{}, err
	}
	var back any
	if err := json.Unmarshal(b, &back); err != nil {
		return Encoded{}, fmt.Errorf("rendered message does not parse back: %w", err)
	}
	return Encoded{
		Input:    connector.Input{Source: source, Content: content},
		Bytes:    b,
		Modeled:  modeled,
		Artifact: &model.Artifact{Kind: "application/json", Bytes: len(b), SHA256: sum(b), Summary: summary, Decoded: string(b)},
	}, nil
}

// readings parses a window of readings: a JSON array, or values separated by
// commas, semicolons or whitespace. Numbers stay numbers; anything else is text.
func readings(s string) ([]any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("window is required")
	}
	if strings.HasPrefix(s, "[") {
		var out []any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("window is not a JSON array: %w", err)
		}
		return out, nil
	}
	var out []any
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
		if n, err := strconv.ParseFloat(tok, 64); err == nil {
			out = append(out, n)
		} else {
			out = append(out, tok)
		}
	}
	if len(out) > 1000 {
		return nil, fmt.Errorf("window is limited to 1000 readings")
	}
	return out, nil
}

// encodeExtra handles the adapters that are not in encode's switch.
func (e Encoder) encodeExtra(ctx context.Context, adapter string, fields map[string]any) (Encoded, error) {
	switch adapter {
	case "json-fields":
		body := map[string]any{"sensor_id": "s1", "unit": "%", "battery": 88}
		raw := strings.TrimSpace(fmt0(fields, "json"))
		if raw == "" {
			return Encoded{}, fmt.Errorf("json is required: a JSON object with the fields to send")
		}
		var over map[string]any
		if err := json.Unmarshal([]byte(raw), &over); err != nil {
			return Encoded{}, fmt.Errorf("json must be one JSON object: %w", err)
		}
		for k, v := range over {
			body[k] = v
		}
		b, _ := json.Marshal(body)
		return jsonArtifact("telemetry/json", string(b), body, false, fmt.Sprintf("JSON message, %d fields", len(body)))

	case "sensor-value":
		body := map[string]any{"channel": "sensor", "metric": orDefault(str(fields, "metric"), "level")}
		level, err := strconv.ParseFloat(strings.TrimSpace(fmt0(fields, "level")), 64)
		if err != nil {
			return Encoded{}, fmt.Errorf("level is required and must be a number")
		}
		body["level"] = level
		if u := str(fields, "unit"); u != "" {
			body["unit"] = u
		}
		if n := str(fields, "note"); n != "" {
			body["note"] = n
		}
		b, _ := json.Marshal(body)
		return jsonArtifact("sensor/value", string(b), body, true, "modeled: one reading sent as JSON")

	case "reading-stream":
		win, err := readings(str(fields, "window"))
		if err != nil {
			return Encoded{}, err
		}
		body := map[string]any{"channel": "control", "window": win}
		if n := num(fields, "interval_ms", 0); n > 0 {
			body["interval_ms"] = n
		}
		b, _ := json.Marshal(body)
		return jsonArtifact("sensor/stream", string(b), body, true, fmt.Sprintf("modeled: window of %d readings sent as JSON", len(win)))

	case "binary-toggle":
		pulses, err := signal.BitPulses(fmt0(fields, "bits"))
		if err != nil {
			return Encoded{}, err
		}
		unit := num(fields, "unit_ms", 200)
		if unit < 50 || unit > 2000 {
			return Encoded{}, fmt.Errorf("unit_ms %d is outside 50..2000", unit)
		}
		bits := signal.PulseBits(pulses)
		wav := signal.MorseWAV(pulses, unit, 700)
		return Encoded{
			Input: connector.Input{Source: "gpio/binary", Content: bits, BlinkUnits: signal.BlinkUnitsJSON(pulses), UnitMs: unit},
			Bytes: wav,
			Artifact: &model.Artifact{
				Kind: "pulses", Bytes: len(pulses), SHA256: sum([]byte(bits)),
				Summary: fmt.Sprintf("%d bits, %d steps, %d ms unit, %.1f s", len(bits), len(pulses), unit, float64(signal.PulsesDuration(pulses, unit))/1000),
				Decoded: bits,
			},
		}, nil

	case "ble-advert":
		adv := signal.BLEAdvert{Name: str(fields, "name"), Manufacturer: str(fields, "manufacturer"), Extended: boolean(fields, "extended")}
		ad, err := signal.BLEAdvertising(adv)
		if err != nil {
			return Encoded{}, err
		}
		back, derr := signal.BLEDecode(ad)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered advertisement does not decode: %w", derr)
		}
		kind := "legacy"
		if adv.Extended {
			kind = "extended"
		}
		return Encoded{
			Input: connector.Input{Source: "ble/advert", Content: back.Text()},
			Bytes: ad,
			Artifact: &model.Artifact{Kind: "application/x-ble-ad", Bytes: len(ad), SHA256: sum(ad),
				Summary: fmt.Sprintf("%s advertising data, %d bytes: %s", kind, len(ad), hex.EncodeToString(ad)),
				Decoded: back.Text()},
		}, nil

	case "nfc-tag":
		var recs []signal.NDEFRecord
		if t := str(fields, "text"); t != "" {
			recs = append(recs, signal.NDEFRecord{Kind: "text", Lang: str(fields, "lang"), Text: t})
		}
		if u := str(fields, "uri"); u != "" {
			recs = append(recs, signal.NDEFRecord{Kind: "uri", URI: u})
		}
		if len(recs) == 0 {
			return Encoded{}, fmt.Errorf("text or uri is required")
		}
		ndef, err := signal.NDEF(recs)
		if err != nil {
			return Encoded{}, err
		}
		back, derr := signal.NDEFDecode(ndef)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered NDEF does not decode: %w", derr)
		}
		text := signal.NDEFText(back)
		return Encoded{
			Input: connector.Input{Source: "nfc/ndef", Content: text},
			Bytes: ndef,
			Artifact: &model.Artifact{Kind: "application/x-ndef", Bytes: len(ndef), SHA256: sum(ndef),
				Summary: fmt.Sprintf("NDEF, %d records, %d bytes, %s", len(back), len(ndef), signal.NDEFCapacity(len(ndef))),
				Decoded: text},
		}, nil

	case "ocr-text":
		t := str(fields, "text")
		if t == "" {
			return Encoded{}, fmt.Errorf("text is required")
		}
		opt := signal.TextImageOptions{Contrast: fnum(fields, "contrast", 1), Scale: num(fields, "scale", 2)}
		png, err := signal.TextImagePNG(t, opt)
		if err != nil {
			return Encoded{}, err
		}
		return image("camera/ocr", t, png, fmt.Sprintf("scanned text, contrast %.2f", opt.Contrast), ""), nil

	case "agent-note":
		t := str(fields, "text")
		if t == "" {
			return Encoded{}, fmt.Errorf("text is required")
		}
		body := map[string]any{"channel": "inter-agent", "origin": "agent-note", "context": t}
		enc, err := jsonArtifact("agent/note", t, body, true, "modeled: note sent as text")
		return enc, err

	case "retrieved-doc":
		t := str(fields, "text")
		if t == "" {
			return Encoded{}, fmt.Errorf("text is required")
		}
		body := map[string]any{"channel": "inter-agent", "origin": "retrieved-doc", "title": orDefault(str(fields, "title"), "Knowledge base article"),
			"source": orDefault(str(fields, "url"), "https://kb.example.test/article"), "score": fnum(fields, "score", 0.87), "context": t}
		return jsonArtifact("rag/document", t, body, true, "modeled: retrieved chunk sent as JSON")

	case "voice-transcript":
		t := str(fields, "spoken")
		if t == "" {
			return Encoded{}, fmt.Errorf("spoken is required")
		}
		body := map[string]any{"channel": "voice", "transcript": t, "confidence": fnum(fields, "confidence", 0.94), "language": orDefault(str(fields, "language"), "en")}
		return jsonArtifact("voice/transcript", t, body, true, "modeled: transcript sent as text, no audio")

	case "audio-upload":
		b64 := strings.TrimSpace(fmt0(fields, "audio_base64"))
		if b64 == "" {
			return Encoded{}, fmt.Errorf("a WAV is required: upload a file or record one")
		}
		wav, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return Encoded{}, fmt.Errorf("audio_base64 is not valid base64")
		}
		if len(wav) > maxUploadAudio {
			return Encoded{}, fmt.Errorf("audio is %d bytes; the limit is 6 MiB", len(wav))
		}
		if !bytes.HasPrefix(wav, []byte("RIFF")) {
			return Encoded{}, fmt.Errorf("only WAV files are accepted")
		}
		info, err := signal.WAVInfo(wav)
		if err != nil {
			return Encoded{}, fmt.Errorf("audio is not a usable WAV: %w", err)
		}
		name := str(fields, "filename")
		if strings.ContainsAny(name, "/\\\x00") {
			return Encoded{}, fmt.Errorf("filename cannot contain path separators")
		}
		summary := "uploaded WAV"
		if name != "" {
			summary += " " + name
		}
		return audio("microphone/audio", str(fields, "text"), wav, summary, info.String()), nil
	}
	return Encoded{}, fmt.Errorf("unknown adapter %q", adapter)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// fmt0 reads a field without trimming, so JSON and bit strings keep their text.
func fmt0(f map[string]any, k string) string {
	switch v := f[k].(type) {
	case string:
		return v
	case nil:
		return ""
	}
	return fmt.Sprint(f[k])
}
