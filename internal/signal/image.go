package signal

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/makiuchi-d/gozxing"
	qrreader "github.com/makiuchi-d/gozxing/qrcode"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// QRPNG renders text as a QR code. Size is the image edge in pixels. Payloads
// above the QR capacity for medium error correction (~2300 bytes) are rejected
// with the library's error.
func QRPNG(text string, size int) ([]byte, error) {
	if size < 64 {
		size = 256
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty QR content")
	}
	return qrcode.Encode(text, qrcode.Medium, size)
}

// QRDecode reads a QR code out of a PNG/JPEG image. The reference optical
// target uses it, so the thing under test decodes pixels, not a label.
func QRDecode(img []byte) (string, error) {
	decoded, _, err := image.Decode(bytes.NewReader(img))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(decoded)
	if err != nil {
		return "", err
	}
	res, err := qrreader.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", fmt.Errorf("no QR code found: %w", err)
	}
	return res.GetText(), nil
}

// TextImageOptions controls how text is rasterised for an OCR/vision target.
//
// Contrast is 0..1: 1 is black on white, 0.1 is barely visible to a person but
// still read by most OCR pipelines. That gap is the realistic attack surface,
// so it is a first-class parameter rather than a hidden trick.
type TextImageOptions struct {
	Width    int
	Contrast float64
	Scale    int
	Wrap     int
}

// TextImagePNG renders text into a PNG with the bundled 7x13 bitmap font.
func TextImagePNG(text string, opt TextImageOptions) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty image text")
	}
	if opt.Wrap <= 0 {
		opt.Wrap = 60
	}
	if opt.Scale <= 0 {
		opt.Scale = 2
	}
	if opt.Contrast <= 0 || opt.Contrast > 1 {
		opt.Contrast = 1
	}
	lines := wrap(text, opt.Wrap)
	face := basicfont.Face7x13
	lineH := face.Height + 4
	w := opt.Wrap*face.Advance + 20
	if opt.Width > w {
		w = opt.Width
	}
	h := len(lines)*lineH + 20
	base := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{255, 255, 255, 255}
	level := uint8(255 - 255*opt.Contrast)
	fg := color.RGBA{level, level, level, 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base.Set(x, y, bg)
		}
	}
	d := &font.Drawer{Dst: base, Src: image.NewUniform(fg), Face: face}
	for i, line := range lines {
		d.Dot = fixed.P(10, 10+face.Ascent+i*lineH)
		d.DrawString(line)
	}
	out := base
	if opt.Scale > 1 {
		out = image.NewRGBA(image.Rect(0, 0, w*opt.Scale, h*opt.Scale))
		for y := 0; y < h*opt.Scale; y++ {
			for x := 0; x < w*opt.Scale; x++ {
				out.Set(x, y, base.At(x/opt.Scale, y/opt.Scale))
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func wrap(text string, width int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if len(cur)+1+len(w) > width {
				lines = append(lines, cur)
				cur = w
				continue
			}
			cur += " " + w
		}
		lines = append(lines, cur)
	}
	return lines
}
