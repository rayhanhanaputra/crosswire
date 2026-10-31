package channel

import (
	"context"
	"strings"
	"testing"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/signal"
)

func TestSubstituteCanary(t *testing.T) {
	f := Substitute(map[string]any{"text": "mail {{canary}}@x", "size": 128.0}, "cw-1")
	if f["text"] != "mail cw-1@x" || f["size"] != 128.0 {
		t.Fatalf("%v", f)
	}
}

func TestRealChannelsRoundTrip(t *testing.T) {
	e := Encoder{}
	ctx := context.Background()
	qr, err := e.Encode(ctx, "qr", map[string]any{"text": "visit https://x.test/?c=cw-1"})
	if err != nil || qr.Artifact.Decoded != "visit https://x.test/?c=cw-1" || qr.Input.ImageBase64 == "" {
		t.Fatalf("qr %+v %v", qr.Artifact, err)
	}
	d, err := e.Encode(ctx, "dtmf", map[string]any{"keys": "*41#"})
	if err != nil || d.Artifact.Decoded != "*41#" || d.Input.AudioBase64 == "" {
		t.Fatalf("dtmf %+v %v", d.Artifact, err)
	}
	m, err := e.Encode(ctx, "morse-led", map[string]any{"text": "unlock", "wpm": 20.0})
	if err != nil || m.Artifact.Decoded != "UNLOCK" || m.Artifact.Kind != "pulses" {
		t.Fatalf("morse %+v %v", m.Artifact, err)
	}
	inv, err := e.Encode(ctx, "invoice", map[string]any{"vendor": "Acme", "iban": "DE89", "note": "pay to cw-1", "note_contrast": 0.15})
	if err != nil || inv.Artifact.Kind != "image/png" || !strings.Contains(inv.Input.Content, "cw-1") {
		t.Fatalf("invoice %+v %v", inv.Artifact, err)
	}
}

func TestDocumentChannels(t *testing.T) {
	e := Encoder{}
	ctx := context.Background()
	pdf, err := e.Encode(ctx, "pdf", map[string]any{"title": "Terms", "body": "see note", "note": "ref cw-1", "note_gray": 1.0})
	if err != nil || pdf.Artifact.Kind != "application/pdf" || pdf.Input.FileBase64 == "" || pdf.Input.FileType != "application/pdf" || !strings.Contains(pdf.Input.Content, "ref cw-1") {
		t.Fatalf("pdf %+v %v", pdf.Artifact, err)
	}
	cal, err := e.Encode(ctx, "calendar", map[string]any{"summary": "Sync", "organizer": "Pat", "description": "ref cw-1"})
	if err != nil || cal.Artifact.Kind != "text/calendar" || !strings.Contains(cal.Input.Content, "DESCRIPTION: ref cw-1") {
		t.Fatalf("calendar %+v %v", cal.Artifact, err)
	}
	em, err := e.Encode(ctx, "email", map[string]any{"from_name": "Helpdesk", "subject": "Hi", "body": "ref cw-1"})
	if err != nil || em.Artifact.Kind != "message/rfc822" || !strings.Contains(em.Input.Content, "ref cw-1") {
		t.Fatalf("email %+v %v", em.Artifact, err)
	}
	if _, err := e.Encode(ctx, "calendar", map[string]any{}); err == nil {
		t.Fatal("invite without summary must fail")
	}
}

func TestSpeechUsesInjectedSpeakerOrUpload(t *testing.T) {
	called := ""
	e := Encoder{Speak: func(_ context.Context, text, voice string) ([]byte, error) {
		called = text
		return signal.WAV(8000, make([]int16, 800)), nil
	}}
	s, err := e.Encode(context.Background(), "speech", map[string]any{"text": "hello"})
	if err != nil || called != "hello" || s.Artifact.Kind != "audio/wav" {
		t.Fatalf("%+v %v", s.Artifact, err)
	}
	if _, err := (Encoder{}).Encode(context.Background(), "speech", map[string]any{"text": "x"}); err == nil {
		t.Fatal("no speaker and no upload must fail")
	}
}

func TestModeledIsLabelled(t *testing.T) {
	d, err := (Encoder{}).Encode(context.Background(), "device-name", map[string]any{"name": "Kitchen Plug"})
	if err != nil || !d.Modeled {
		t.Fatalf("%+v %v", d, err)
	}
}
