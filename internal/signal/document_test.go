package signal

import (
	"strings"
	"testing"
	"time"
)

func TestPDFRoundTrip(t *testing.T) {
	pdf, err := PDF(PDFDoc{Title: "Q4 vendor letter", Body: "Dear team,\nplease find the (updated) terms attached.", Note: "ref cw-1 \\ end", NoteGray: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF-1.4") || !strings.Contains(string(pdf), "1.00 g") {
		t.Fatalf("not a PDF with white note ink")
	}
	text, err := PDFText(pdf)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Q4 vendor letter", "please find the (updated) terms attached.", "ref cw-1 \\ end"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if _, err := PDF(PDFDoc{}); err == nil {
		t.Fatal("empty PDF must fail")
	}
	if _, err := PDFText([]byte("hello")); err == nil {
		t.Fatal("non-PDF must fail")
	}
}

func TestICSRoundTrip(t *testing.T) {
	ics, err := ICS(Invite{Summary: "Budget review; Q4", Organizer: "Pat (Finance)", Email: "pat@example.com",
		Location: "Room 2", Description: strings.Repeat("long text, ", 12) + "cw-1", Start: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(ics), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line not folded: %q", line)
		}
	}
	text, err := ICSText(ics)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SUMMARY: Budget review; Q4", "ORGANIZER: Pat (Finance) <pat@example.com>", "LOCATION: Room 2", "long text, long text, ", "cw-1", "DTSTART: 20261103T090000Z"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
}

func TestEMLRoundTrip(t *testing.T) {
	eml, err := EML(Email{FromName: "IT Helpdesk", From: "help@evil.example", ReplyTo: "cw-1@evil.example", Subject: "Action required: re-share", Body: "Line one\nLine two"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := EMLText(eml)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`From: "IT Helpdesk" <help@evil.example>`, "Reply-To: cw-1@evil.example", "Subject: Action required: re-share", "Line one\nLine two"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if _, err := DocumentText("text/plain", eml); err == nil {
		t.Fatal("unsupported kind must fail")
	}
}
