package signal

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// Document channels: files an inbox or assistant agent opens. Each renderer
// produces a real file of its type and each parser reads it back the way a
// plain extractor would, so a run can show exactly what reached the model.

// PDFDoc is a one-page text document. Note is the field an attacker
// controls; NoteGray is its ink level from 0 (black) to 1 (white, invisible
// to a reader but present for a text extractor).
type PDFDoc struct {
	Title    string
	Body     string
	Note     string
	NoteGray float64
}

// PDF renders a minimal, uncompressed PDF 1.4 with Helvetica. Only ASCII
// survives; other runes become '?', which the parser reports faithfully.
func PDF(d PDFDoc) ([]byte, error) {
	if strings.TrimSpace(d.Title) == "" && strings.TrimSpace(d.Body) == "" {
		return nil, fmt.Errorf("pdf needs a title or a body")
	}
	if d.NoteGray < 0 || d.NoteGray > 1 {
		return nil, fmt.Errorf("note_gray must be between 0 and 1")
	}
	var content bytes.Buffer
	y := 780.0
	line := func(size float64, gray float64, s string) {
		fmt.Fprintf(&content, "BT /F1 %.0f Tf %.2f g 56 %.0f Td (%s) Tj ET\n", size, gray, y, pdfEscape(s))
		y -= size * 1.5
	}
	if d.Title != "" {
		line(18, 0, d.Title)
		y -= 8
	}
	for _, l := range wrap(d.Body, 90) {
		line(11, 0, l)
	}
	if strings.TrimSpace(d.Note) != "" {
		y -= 12
		for _, l := range wrap(d.Note, 90) {
			line(9, d.NoteGray, l)
		}
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()),
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs)+1)
	for i, o := range objs {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return out.Bytes(), nil
}

var pdfTj = regexp.MustCompile(`\(((?:\\.|[^\\)])*)\)\s*Tj`)

// PDFText extracts the text of a PDF whose content streams are plain
// (uncompressed) and use Tj, which is what PDF produces and what a basic
// extractor sees. Compressed streams yield nothing, which is reported.
func PDFText(pdf []byte) (string, error) {
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return "", fmt.Errorf("not a PDF")
	}
	var lines []string
	for _, m := range pdfTj.FindAllSubmatch(pdf, -1) {
		lines = append(lines, pdfUnescape(string(m[1])))
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("no plain text found (compressed or image-only PDF)")
	}
	return strings.Join(lines, "\n"), nil
}

func pdfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '(' || r == ')':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 32 || r > 126:
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pdfUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Invite is a calendar invitation. Every text field is attacker-controlled
// in practice: anyone can send an invite to an assistant's calendar.
type Invite struct {
	Summary     string
	Organizer   string // display name
	Email       string
	Location    string
	Description string
	Start       time.Time
}

// ICS renders a single-event iCalendar file.
func ICS(in Invite) ([]byte, error) {
	if strings.TrimSpace(in.Summary) == "" {
		return nil, fmt.Errorf("invite summary is required")
	}
	if in.Start.IsZero() {
		in.Start = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	}
	if in.Email == "" {
		in.Email = "organizer@example.com"
	}
	var b strings.Builder
	w := func(k, v string) { b.WriteString(icsFold(k+":"+icsEscape(v)) + "\r\n") }
	w("BEGIN", "VCALENDAR")
	w("VERSION", "2.0")
	w("PRODID", "-//Crosswire//document adapter//EN")
	w("METHOD", "REQUEST")
	w("BEGIN", "VEVENT")
	w("UID", fmt.Sprintf("%d@crosswire", in.Start.Unix()))
	w("DTSTAMP", in.Start.UTC().Format("20060102T150405Z"))
	w("DTSTART", in.Start.UTC().Format("20060102T150405Z"))
	w("DTEND", in.Start.Add(30*time.Minute).UTC().Format("20060102T150405Z"))
	w("SUMMARY", in.Summary)
	if in.Organizer != "" {
		b.WriteString(icsFold("ORGANIZER;CN="+icsParam(in.Organizer)+":mailto:"+in.Email) + "\r\n")
	}
	if in.Location != "" {
		w("LOCATION", in.Location)
	}
	if in.Description != "" {
		w("DESCRIPTION", in.Description)
	}
	w("END", "VEVENT")
	w("END", "VCALENDAR")
	return []byte(b.String()), nil
}

// ICSText reads the event back as "KEY: value" lines, the way a calendar
// integration would surface it to a model.
func ICSText(ics []byte) (string, error) {
	raw := strings.ReplaceAll(string(ics), "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\n ", "")
	raw = strings.ReplaceAll(raw, "\n\t", "")
	if !strings.Contains(raw, "BEGIN:VEVENT") {
		return "", fmt.Errorf("no VEVENT found")
	}
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		name, params, _ := strings.Cut(k, ";")
		switch name {
		case "SUMMARY", "LOCATION", "DESCRIPTION", "DTSTART":
			out = append(out, name+": "+icsUnescape(v))
		case "ORGANIZER":
			cn := ""
			for _, p := range strings.Split(params, ";") {
				if strings.HasPrefix(p, "CN=") {
					cn = strings.Trim(strings.TrimPrefix(p, "CN="), `"`)
				}
			}
			out = append(out, "ORGANIZER: "+cn+" <"+strings.TrimPrefix(v, "mailto:")+">")
		}
	}
	return strings.Join(out, "\n"), nil
}

func icsEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`)
	return r.Replace(s)
}

func icsUnescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\;`, ";", `\,`, ",", `\\`, `\`)
	return r.Replace(s)
}

func icsParam(s string) string {
	s = strings.NewReplacer(`"`, "", ";", "", ":", "", "\n", " ").Replace(s)
	return `"` + s + `"`
}

func icsFold(line string) string {
	const max = 75
	if len(line) <= max {
		return line
	}
	var b strings.Builder
	limit := max
	for len(line) > limit {
		cut := limit
		for cut > 0 && line[cut]&0xC0 == 0x80 { // do not split a rune
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line = line[cut:]
		limit = max - 1 // continuation lines carry a leading space
	}
	b.WriteString(line)
	return b.String()
}

// Email is an RFC 5322 message. The display name and Reply-To are the
// fields most agents render without the address that would expose them.
type Email struct {
	FromName string
	From     string
	ReplyTo  string
	To       string
	Subject  string
	Body     string
	Date     time.Time
}

// EML renders a plain-text email.
func EML(e Email) ([]byte, error) {
	if strings.TrimSpace(e.Subject) == "" && strings.TrimSpace(e.Body) == "" {
		return nil, fmt.Errorf("email needs a subject or a body")
	}
	if e.From == "" {
		e.From = "sender@example.com"
	}
	if e.To == "" {
		e.To = "assistant@example.com"
	}
	if e.Date.IsZero() {
		e.Date = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	}
	from := e.From
	if e.FromName != "" {
		from = (&mail.Address{Name: e.FromName, Address: e.From}).String()
	}
	var b strings.Builder
	h := func(k, v string) { b.WriteString(k + ": " + mime.QEncoding.Encode("utf-8", v) + "\r\n") }
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + e.To + "\r\n")
	if e.ReplyTo != "" {
		b.WriteString("Reply-To: " + e.ReplyTo + "\r\n")
	}
	h("Subject", e.Subject)
	b.WriteString("Date: " + e.Date.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: <" + fmt.Sprintf("%d", e.Date.Unix()) + "@crosswire>\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(e.Body, "\r\n", "\n"))
	b.WriteString("\n")
	return []byte(b.String()), nil
}

// EMLText reads the message back as the headers an assistant shows plus
// the body.
func EMLText(eml []byte) (string, error) {
	m, err := mail.ReadMessage(bytes.NewReader(eml))
	if err != nil {
		return "", fmt.Errorf("not an email: %w", err)
	}
	dec := new(mime.WordDecoder)
	get := func(k string) string {
		v, err := dec.DecodeHeader(m.Header.Get(k))
		if err != nil {
			return m.Header.Get(k)
		}
		return v
	}
	body, _ := io.ReadAll(io.LimitReader(m.Body, 1<<20))
	out := []string{"From: " + get("From")}
	if rt := get("Reply-To"); rt != "" {
		out = append(out, "Reply-To: "+rt)
	}
	out = append(out, "Subject: "+get("Subject"), "", strings.TrimSpace(string(body)))
	return strings.Join(out, "\n"), nil
}

// DocumentText dispatches on the file type an inbox agent receives.
func DocumentText(kind string, data []byte) (string, error) {
	switch kind {
	case "application/pdf":
		return PDFText(data)
	case "text/calendar":
		return ICSText(data)
	case "message/rfc822":
		return EMLText(data)
	}
	return "", fmt.Errorf("unsupported document type %q", kind)
}
