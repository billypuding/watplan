package ogimage

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
)

func renderer(t *testing.T) *Renderer {
	t.Helper()
	logo, err := os.ReadFile("../server/web/favicon-192.png")
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(logo)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRender(t *testing.T) {
	r := renderer(t)
	data, err := r.Render(Card{Kicker: "Plan zajęć", Title: "WMT26AX3S1", Subtitle: "Wydział Mechatroniki, Uzbrojenia i Lotnictwa", Footer: "za darmo"})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != Width || b.Dy() != Height {
		t.Fatalf("rozmiar %v", b)
	}
	if len(data) > 200<<10 {
		t.Fatalf("grafika ma %d B", len(data))
	}
	other, _ := r.Render(Card{Kicker: "Plan zajęć", Title: "WCY26KY4S1"})
	if bytes.Equal(data, other) {
		t.Fatal("różne karty dały ten sam obraz")
	}
}

func TestFitShrinksAndTruncates(t *testing.T) {
	r := renderer(t)
	f, text := r.fit("WMT26AX3S1", true, 148, 72, 1040)
	if text != "WMT26AX3S1" || width(f, text) > 1040 {
		t.Fatalf("krótki tytuł: %q, %d px", text, width(f, text))
	}
	long := strings.Repeat("Bardzo długa nazwa wydziału ", 6)
	f, text = r.fit(long, false, 42, 30, 1040)
	if !strings.HasSuffix(text, "…") || width(f, text) > 1040 {
		t.Fatalf("długi tekst nie został skrócony: %q, %d px", text, width(f, text))
	}
}

func TestNewRejectsBadLogo(t *testing.T) {
	if _, err := New([]byte("to nie png")); err == nil {
		t.Fatal("zły logotyp powinien dać błąd")
	}
}
