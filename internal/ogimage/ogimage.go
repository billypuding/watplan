package ogimage

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/Geist-SemiBold.ttf fonts/Geist-Medium.ttf
var fontFiles embed.FS

const (
	Width  = 1200
	Height = 630
	margin = 80
)

var (
	background = color.RGBA{0x0e, 0x0f, 0x11, 0xff}
	textColor  = color.RGBA{0xed, 0xee, 0xf0, 0xff}
	mutedColor = color.RGBA{0x86, 0x8b, 0x94, 0xff}
	accent     = color.RGBA{0xeb, 0xe5, 0xd3, 0xff}
	panel      = color.RGBA{0x15, 0x17, 0x1a, 0xff}
)

type Card struct {
	Kicker   string
	Title    string
	Subtitle string
	Footer   string
}

type Renderer struct {
	semibold *opentype.Font
	medium   *opentype.Font
	logo     image.Image
	mu       sync.Mutex
	faces    map[faceKey]font.Face
}

type faceKey struct {
	bold bool
	size float64
}

func New(logoPNG []byte) (*Renderer, error) {
	load := func(name string) (*opentype.Font, error) {
		data, err := fontFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		return opentype.Parse(data)
	}
	semibold, err := load("fonts/Geist-SemiBold.ttf")
	if err != nil {
		return nil, err
	}
	medium, err := load("fonts/Geist-Medium.ttf")
	if err != nil {
		return nil, err
	}
	r := &Renderer{semibold: semibold, medium: medium, faces: map[faceKey]font.Face{}}
	if len(logoPNG) > 0 {
		if r.logo, err = png.Decode(bytes.NewReader(logoPNG)); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Renderer) face(bold bool, size float64) font.Face {
	key := faceKey{bold, size}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.faces[key]; ok {
		return f
	}
	src := r.medium
	if bold {
		src = r.semibold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	r.faces[key] = f
	return f
}

func width(f font.Face, text string) int {
	return font.MeasureString(f, text).Ceil()
}

func (r *Renderer) fit(text string, bold bool, size, min float64, max int) (font.Face, string) {
	for ; size > min; size -= 4 {
		if f := r.face(bold, size); width(f, text) <= max {
			return f, text
		}
	}
	f := r.face(bold, min)
	runes := []rune(text)
	for len(runes) > 1 && width(f, string(runes)+"…") > max {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == len([]rune(text)) {
		return f, text
	}
	return f, strings.TrimRight(string(runes), " ,") + "…"
}

func (r *Renderer) write(img draw.Image, f font.Face, text string, x, y int, c color.Color) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

func (r *Renderer) Render(card Card) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, Width, Height))
	draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, Height-12, Width, Height), image.NewUniform(accent), image.Point{}, draw.Src)

	brandX := margin
	if r.logo != nil {
		draw.CatmullRom.Scale(img, image.Rect(margin, 64, margin+64, 128), r.logo, r.logo.Bounds(), draw.Over, nil)
		brandX += 84
	}
	r.write(img, r.face(true, 40), "WATplan", brandX, 110, textColor)
	site := r.face(false, 30)
	r.write(img, site, "watplan.me", Width-margin-width(site, "watplan.me"), 106, mutedColor)

	inner := Width - 2*margin
	if card.Kicker != "" {
		r.write(img, r.face(false, 44), card.Kicker, margin, 262, mutedColor)
	}
	title, text := r.fit(card.Title, true, 148, 72, inner)
	r.write(img, title, text, margin, 400, textColor)
	if card.Subtitle != "" {
		sub, text := r.fit(card.Subtitle, false, 42, 30, inner)
		r.write(img, sub, text, margin, 472, accent)
	}
	if card.Footer != "" {
		draw.Draw(img, image.Rect(margin, 520, Width-margin, 521), image.NewUniform(panel), image.Point{}, draw.Src)
		foot, text := r.fit(card.Footer, false, 30, 24, inner)
		r.write(img, foot, text, margin, 574, mutedColor)
	}

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
