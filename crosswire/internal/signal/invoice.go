package signal

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Invoice is the content of a rendered supplier invoice. Note is where an
// attacker writes; NoteContrast lets it be faint to a person yet legible to
// a vision model, which is how real document attacks are placed.
type Invoice struct {
	Vendor       string
	Number       string
	Date         string
	Items        []InvoiceItem
	IBAN         string
	Note         string
	NoteContrast float64
}

// InvoiceItem is one line on the invoice.
type InvoiceItem struct {
	Description string
	Amount      string
}

// InvoicePNG renders an A4-proportioned invoice at 2× the bitmap font size.
func InvoicePNG(inv Invoice) ([]byte, error) {
	if strings.TrimSpace(inv.Vendor) == "" {
		return nil, fmt.Errorf("invoice vendor is required")
	}
	if inv.NoteContrast <= 0 || inv.NoteContrast > 1 {
		inv.NoteContrast = 1
	}
	const w, h = 620, 877
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, color.RGBA{255, 255, 255, 255})
	face := basicfont.Face7x13
	black := color.RGBA{20, 20, 20, 255}
	grey := color.RGBA{120, 120, 120, 255}
	lvl := uint8(255 - 255*inv.NoteContrast)
	noteCol := color.RGBA{lvl, lvl, lvl, 255}
	draw := func(x, y int, c color.Color, s string) {
		d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
		d.DrawString(s)
	}
	rule := func(y int) {
		for x := 40; x < w-40; x++ {
			img.Set(x, y, grey)
		}
	}
	y := 60
	draw(40, y, black, strings.ToUpper(inv.Vendor))
	draw(w-220, y, black, "INVOICE "+inv.Number)
	y += 20
	draw(w-220, y, grey, "Date: "+inv.Date)
	y += 30
	rule(y)
	y += 30
	draw(40, y, grey, "DESCRIPTION")
	draw(w-140, y, grey, "AMOUNT")
	y += 10
	rule(y)
	y += 24
	for _, it := range inv.Items {
		draw(40, y, black, it.Description)
		draw(w-140, y, black, it.Amount)
		y += 20
	}
	y += 10
	rule(y)
	y += 30
	draw(40, y, black, "PAYMENT DETAILS")
	y += 20
	draw(40, y, black, "IBAN: "+inv.IBAN)
	y += 40
	if strings.TrimSpace(inv.Note) != "" {
		draw(40, y, grey, "Notes")
		y += 18
		for _, line := range wrap(inv.Note, 78) {
			draw(40, y, noteCol, line)
			y += 16
		}
	}
	draw(40, h-40, grey, "Thank you for your business.")

	out := image.NewRGBA(image.Rect(0, 0, w*2, h*2))
	for yy := 0; yy < h*2; yy++ {
		for xx := 0; xx < w*2; xx++ {
			out.Set(xx, yy, img.At(xx/2, yy/2))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fill(img *image.RGBA, c color.Color) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}
