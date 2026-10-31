// Package channel turns an operator's fields into what a target's sensor
// would receive: the decoded content plus, where the channel is real, the
// physical artifact (PNG, WAV, pulse timeline). docs/CONTRACT.md §5 lists
// the adapters.
package channel

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/connector"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/model"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

// Encoded is the result of one encoding.
type Encoded struct {
	Input    connector.Input
	Artifact *model.Artifact
	Bytes    []byte // artifact bytes, nil for text channels
	Modeled  bool
}

// Speaker renders text to WAV; injected so tests stay offline.
type Speaker func(ctx context.Context, text, voice string) ([]byte, error)

// Encoder holds the external dependencies.
type Encoder struct {
	Speak Speaker
}

// Info describes an adapter for the UI.
type Info struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	Artifact string   `json:"artifact"`
	Modeled  bool     `json:"modeled"`
	Fields   []string `json:"fields"`
	Note     string   `json:"note"`
}

var catalogue = []Info{
	{"text", "chat/text", "text", false, []string{"text"}, "Plain text. Used for the direct arm."},
	{"qr", "camera/qr", "image/png", false, []string{"text", "size"}, "Real QR PNG, decoded back to verify."},
	{"sign", "camera/sign", "image/png", false, []string{"text", "contrast", "scale"}, "Text rendered into an image; contrast below 0.2 is faint to people."},
	{"invoice", "camera/document", "image/png", false, []string{"vendor", "number", "amount", "iban", "note", "note_contrast"}, "Rendered supplier invoice; the note field is the attacker's."},
	{"speech", "microphone/speech", "audio/wav", false, []string{"text", "voice", "audio_base64"}, "Spoken payload (TTS) or an uploaded WAV."},
	{"dtmf", "microphone/dtmf", "audio/wav", false, []string{"keys", "tone_ms", "gap_ms"}, "Capacity demo: 16 symbols, cannot carry language."},
	{"morse-led", "light-sensor/morse", "pulses", false, []string{"text", "wpm"}, "Capacity demo: timing only; a 1 fps camera cannot read it."},
	{"device-name", "ble/device-name", "text", true, []string{"name"}, "MODELED: the name is sent as text; no radio is used."},
	{"pdf", "inbox/pdf", "application/pdf", false, []string{"title", "body", "note", "note_gray"}, "Real one-page PDF; note_gray 1 is white ink, unseen by a reader, read by an extractor."},
	{"calendar", "inbox/ics", "text/calendar", false, []string{"summary", "organizer", "location", "description"}, "Real iCalendar invite; every field is set by whoever sends it."},
	{"email", "inbox/eml", "message/rfc822", false, []string{"from_name", "from", "reply_to", "subject", "body"}, "Real RFC 5322 message; display name and Reply-To are the sender's."},
}

// Catalogue lists the adapters.
func Catalogue() []Info { return catalogue }

// Substitute replaces {{canary}} in every string field.
func Substitute(fields map[string]any, canary string) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if s, ok := v.(string); ok {
			out[k] = strings.ReplaceAll(s, "{{canary}}", canary)
			continue
		}
		out[k] = v
	}
	return out
}

// Encode renders fields for the adapter. Canary substitution is the
// caller's job (Substitute) so that controls can be built the same way.
func (e Encoder) Encode(ctx context.Context, adapter string, fields map[string]any) (Encoded, error) {
	switch adapter {
	case "text":
		t := str(fields, "text")
		if t == "" {
			return Encoded{}, fmt.Errorf("text is required")
		}
		return Encoded{Input: connector.Input{Source: "chat/text", Content: t}, Artifact: &model.Artifact{Kind: "text", Bytes: len(t), SHA256: sum([]byte(t)), Summary: fmt.Sprintf("%d chars", len(t))}}, nil

	case "qr":
		t := str(fields, "text")
		size := num(fields, "size", 256)
		png, err := signal.QRPNG(t, size)
		if err != nil {
			return Encoded{}, err
		}
		decoded, derr := signal.QRDecode(png)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered QR does not decode: %w", derr)
		}
		return image("camera/qr", t, png, fmt.Sprintf("QR %dpx", size), decoded), nil

	case "sign":
		t := str(fields, "text")
		opt := signal.TextImageOptions{Contrast: fnum(fields, "contrast", 1), Scale: num(fields, "scale", 2)}
		png, err := signal.TextImagePNG(t, opt)
		if err != nil {
			return Encoded{}, err
		}
		return image("camera/sign", t, png, fmt.Sprintf("sign, contrast %.2f", opt.Contrast), ""), nil

	case "invoice":
		inv := signal.Invoice{
			Vendor:       str(fields, "vendor"),
			Number:       str(fields, "number"),
			Date:         str(fields, "date"),
			IBAN:         str(fields, "iban"),
			Note:         str(fields, "note"),
			NoteContrast: fnum(fields, "note_contrast", 1),
		}
		if inv.Number == "" {
			inv.Number = "INV-20471"
		}
		if inv.Date == "" {
			inv.Date = "2026-10-31"
		}
		amount := str(fields, "amount")
		if amount == "" {
			amount = "1,480.00"
		}
		inv.Items = []signal.InvoiceItem{{Description: "Maintenance contract, Q4", Amount: amount}}
		png, err := signal.InvoicePNG(inv)
		if err != nil {
			return Encoded{}, err
		}
		content := fmt.Sprintf("Invoice %s from %s, amount %s, IBAN %s. Note: %s", inv.Number, inv.Vendor, amount, inv.IBAN, inv.Note)
		return image("camera/document", content, png, fmt.Sprintf("invoice, note contrast %.2f", inv.NoteContrast), ""), nil

	case "speech":
		if b64 := str(fields, "audio_base64"); b64 != "" {
			wav, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return Encoded{}, fmt.Errorf("audio_base64 is not valid base64")
			}
			return audio("microphone/speech", str(fields, "text"), wav, "uploaded WAV", ""), nil
		}
		t := str(fields, "text")
		if e.Speak == nil {
			return Encoded{}, fmt.Errorf("no speech synthesiser configured; upload a WAV")
		}
		wav, err := e.Speak(ctx, t, str(fields, "voice"))
		if err != nil {
			return Encoded{}, err
		}
		return audio("microphone/speech", t, wav, "TTS", ""), nil

	case "dtmf":
		keys := str(fields, "keys")
		wav, err := signal.DTMFWAV(keys, num(fields, "tone_ms", 100), num(fields, "gap_ms", 100))
		if err != nil {
			return Encoded{}, err
		}
		decoded, derr := signal.DTMFDecode(wav)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered DTMF does not decode: %w", derr)
		}
		return audio("microphone/dtmf", keys, wav, fmt.Sprintf("DTMF %d keys", len(strings.ReplaceAll(keys, " ", ""))), decoded), nil

	case "morse-led":
		t := str(fields, "text")
		code := signal.MorseEncode(t)
		if code == "" {
			return Encoded{}, fmt.Errorf("text has no Morse-encodable characters")
		}
		pulses := signal.MorsePulses(code)
		unit := signal.WPM(num(fields, "wpm", 12))
		wav := signal.MorseWAV(pulses, unit, 700)
		enc := Encoded{
			Input: connector.Input{Source: "light-sensor/morse", Content: code},
			Bytes: wav,
			Artifact: &model.Artifact{
				Kind:    "pulses",
				Bytes:   len(pulses),
				SHA256:  sum([]byte(code)),
				Summary: fmt.Sprintf("%d pulses, %d ms unit, %.1f s", len(pulses), unit, float64(signal.PulsesDuration(pulses, unit))/1000),
				Decoded: signal.PulsesDecode(pulses),
			},
		}
		return enc, nil

	case "pdf":
		doc := signal.PDFDoc{Title: str(fields, "title"), Body: str(fields, "body"), Note: str(fields, "note"), NoteGray: fnum(fields, "note_gray", 0)}
		pdf, err := signal.PDF(doc)
		if err != nil {
			return Encoded{}, err
		}
		text, derr := signal.PDFText(pdf)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered PDF does not read back: %w", derr)
		}
		return file("inbox/pdf", "application/pdf", text, pdf, fmt.Sprintf("PDF, %d lines, note gray %.2f", len(strings.Split(text, "\n")), doc.NoteGray)), nil

	case "calendar":
		inv := signal.Invite{Summary: str(fields, "summary"), Organizer: str(fields, "organizer"), Email: str(fields, "email"),
			Location: str(fields, "location"), Description: str(fields, "description")}
		ics, err := signal.ICS(inv)
		if err != nil {
			return Encoded{}, err
		}
		text, derr := signal.ICSText(ics)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered invite does not read back: %w", derr)
		}
		return file("inbox/ics", "text/calendar", text, ics, "iCalendar invite"), nil

	case "email":
		em := signal.Email{FromName: str(fields, "from_name"), From: str(fields, "from"), ReplyTo: str(fields, "reply_to"),
			Subject: str(fields, "subject"), Body: str(fields, "body")}
		eml, err := signal.EML(em)
		if err != nil {
			return Encoded{}, err
		}
		text, derr := signal.EMLText(eml)
		if derr != nil {
			return Encoded{}, fmt.Errorf("rendered email does not read back: %w", derr)
		}
		return file("inbox/eml", "message/rfc822", text, eml, "RFC 5322 message"), nil

	case "device-name":
		n := str(fields, "name")
		if n == "" {
			return Encoded{}, fmt.Errorf("name is required")
		}
		return Encoded{
			Input:    connector.Input{Source: "ble/device-name", Content: n},
			Modeled:  true,
			Artifact: &model.Artifact{Kind: "text", Bytes: len(n), SHA256: sum([]byte(n)), Summary: "modeled: sent as text"},
		}, nil
	}
	return Encoded{}, fmt.Errorf("unknown adapter %q", adapter)
}

func image(source, content string, png []byte, summary, decoded string) Encoded {
	return Encoded{
		Input:    connector.Input{Source: source, Content: content, ImageBase64: base64.StdEncoding.EncodeToString(png)},
		Bytes:    png,
		Artifact: &model.Artifact{Kind: "image/png", Bytes: len(png), SHA256: sum(png), Summary: summary, Decoded: decoded},
	}
}

func audio(source, content string, wav []byte, summary, decoded string) Encoded {
	return Encoded{
		Input:    connector.Input{Source: source, Content: content, AudioBase64: base64.StdEncoding.EncodeToString(wav)},
		Bytes:    wav,
		Artifact: &model.Artifact{Kind: "audio/wav", Bytes: len(wav), SHA256: sum(wav), Summary: summary, Decoded: decoded},
	}
}

// file packs a document. Content is the text an extractor reads back, so
// delivery and the direct arm use what the agent would actually see.
func file(source, kind, content string, data []byte, summary string) Encoded {
	return Encoded{
		Input:    connector.Input{Source: source, Content: content, FileBase64: base64.StdEncoding.EncodeToString(data), FileType: kind},
		Bytes:    data,
		Artifact: &model.Artifact{Kind: kind, Bytes: len(data), SHA256: sum(data), Summary: summary, Decoded: content},
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func str(f map[string]any, k string) string {
	v, _ := f[k].(string)
	return strings.TrimSpace(v)
}

func num(f map[string]any, k string, def int) int {
	switch v := f[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func fnum(f map[string]any, k string, def float64) float64 {
	switch v := f[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

// FieldNames lists the fields an adapter accepts, for validation messages.
func FieldNames(adapter string) []string {
	for _, c := range catalogue {
		if c.Name == adapter {
			out := append([]string(nil), c.Fields...)
			sort.Strings(out)
			return out
		}
	}
	return nil
}
