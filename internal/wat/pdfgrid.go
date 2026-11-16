package wat

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

const pdfTol = 1.0

type pdfSegment struct {
	pos, from, to float64
}

type pdfBox struct {
	x0, y0, x1, y1 float64
	color          string
}

type pdfLine struct {
	text string
	bold bool
}

type pdfCell struct {
	page           int
	x0, y0, x1, y1 float64
	lines          []pdfLine
	color          string
}

func (c pdfCell) text(sep string) string {
	parts := make([]string, 0, len(c.lines))
	for _, l := range c.lines {
		parts = append(parts, l.text)
	}
	return strings.TrimSpace(strings.Join(parts, sep))
}

func (c pdfCell) midX() float64 { return (c.x0 + c.x1) / 2 }
func (c pdfCell) midY() float64 { return (c.y0 + c.y1) / 2 }

type pdfGlyph struct {
	x, y, size float64
	s          string
	bold       bool
}

type pdfMatrix [6]float64

func (m pdfMatrix) mul(n pdfMatrix) pdfMatrix {
	return pdfMatrix{
		m[0]*n[0] + m[1]*n[2],
		m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2],
		m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4],
		m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m pdfMatrix) apply(x, y float64) (float64, float64) {
	return x*m[0] + y*m[2] + m[4], x*m[1] + y*m[3] + m[5]
}

func readPDFCells(data []byte) (cells []pdfCell, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("uszkodzony PDF: %v", r)
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		vlines, hlines, boxes := pdfShapes(page)
		cells = append(cells, pdfPageCells(i, pdfGlyphs(page), vlines, hlines, boxes)...)
	}
	return cells, nil
}

func pdfGlyphs(page pdf.Page) []pdfGlyph {
	content := page.Content()
	glyphs := make([]pdfGlyph, 0, len(content.Text))
	for _, t := range content.Text {
		bold := strings.Contains(t.Font, "Pogrubiona") || strings.Contains(strings.ToLower(t.Font), "bold")
		glyphs = append(glyphs, pdfGlyph{x: t.X + t.W/2, y: t.Y + t.FontSize*0.3, size: t.FontSize, s: t.S, bold: bold})
	}
	return glyphs
}

func pdfShapes(page pdf.Page) (vlines, hlines []pdfSegment, boxes []pdfBox) {
	ctm := pdfMatrix{1, 0, 0, 1, 0, 0}
	var stack []pdfMatrix
	color := "#000000"
	var colorStack []string
	var path []pdfBox

	pdf.Interpret(page.V.Key("Contents"), func(stk *pdf.Stack, op string) {
		args := make([]float64, stk.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stk.Pop().Float64()
		}
		switch op {
		case "q":
			stack = append(stack, ctm)
			colorStack = append(colorStack, color)
		case "Q":
			if n := len(stack); n > 0 {
				ctm, stack = stack[n-1], stack[:n-1]
				color, colorStack = colorStack[n-1], colorStack[:n-1]
			}
		case "cm":
			if len(args) == 6 {
				ctm = pdfMatrix{args[0], args[1], args[2], args[3], args[4], args[5]}.mul(ctm)
			}
		case "rg", "sc", "scn":
			if len(args) == 3 {
				color = pdfColor(args[0], args[1], args[2])
			}
		case "g":
			if len(args) == 1 {
				color = pdfColor(args[0], args[0], args[0])
			}
		case "re":
			if len(args) == 4 {
				ax, ay := ctm.apply(args[0], args[1])
				bx, by := ctm.apply(args[0]+args[2], args[1]+args[3])
				path = append(path, pdfBox{math.Min(ax, bx), math.Min(ay, by), math.Max(ax, bx), math.Max(ay, by), color})
			}
		case "f", "f*", "F", "B", "B*", "b", "b*":
			for _, r := range path {
				r.color = color
				switch {
				case r.x1-r.x0 < 2 && r.y1-r.y0 >= 2:
					vlines = append(vlines, pdfSegment{(r.x0 + r.x1) / 2, r.y0, r.y1})
				case r.y1-r.y0 < 2 && r.x1-r.x0 >= 2:
					hlines = append(hlines, pdfSegment{(r.y0 + r.y1) / 2, r.x0, r.x1})
				case r.x1-r.x0 >= 2 && r.y1-r.y0 >= 2:
					boxes = append(boxes, r)
				}
			}
			path = path[:0]
		case "n", "S", "s":
			path = path[:0]
		}
	})
	return vlines, hlines, boxes
}

func pdfColor(r, g, b float64) string {
	c := func(v float64) int { return int(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	return fmt.Sprintf("#%02X%02X%02X", c(r), c(g), c(b))
}

func pdfPageCells(page int, glyphs []pdfGlyph, vlines, hlines []pdfSegment, boxes []pdfBox) []pdfCell {
	type key struct{ x0, y0, x1, y1 int }
	round := func(v float64) int { return int(math.Round(v * 10)) }

	byCell := map[key]*pdfCell{}
	glyphsOf := map[key][]pdfGlyph{}
	var order []key

	for _, g := range glyphs {
		left, okL := nearestSegment(vlines, g.x, g.y, -1)
		right, okR := nearestSegment(vlines, g.x, g.y, 1)
		bottom, okB := nearestSegment(hlines, g.y, g.x, -1)
		top, okT := nearestSegment(hlines, g.y, g.x, 1)
		if !okL || !okR || !okB || !okT {
			continue
		}
		k := key{round(left), round(bottom), round(right), round(top)}
		if _, ok := byCell[k]; !ok {
			byCell[k] = &pdfCell{page: page, x0: left, y0: bottom, x1: right, y1: top}
			order = append(order, k)
		}
		glyphsOf[k] = append(glyphsOf[k], g)
	}

	cells := make([]pdfCell, 0, len(order))
	for _, k := range order {
		cell := byCell[k]
		cell.lines = pdfTextLines(glyphsOf[k])
		if len(cell.lines) == 0 {
			continue
		}
		cell.color = cellColor(*cell, boxes)
		cells = append(cells, *cell)
	}
	return cells
}

func nearestSegment(segs []pdfSegment, at, along float64, dir int) (float64, bool) {
	best, found := 0.0, false
	for _, s := range segs {
		if along < s.from-pdfTol || along > s.to+pdfTol {
			continue
		}
		d := (s.pos - at) * float64(dir)
		if d < 0 {
			continue
		}
		if !found || d < (best-at)*float64(dir) {
			best, found = s.pos, true
		}
	}
	return best, found
}

func pdfTextLines(glyphs []pdfGlyph) []pdfLine {
	sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].y > glyphs[j].y })

	var groups [][]pdfGlyph
	for _, g := range glyphs {
		if n := len(groups); n > 0 && groups[n-1][0].y-g.y < g.size*0.4 {
			groups[n-1] = append(groups[n-1], g)
			continue
		}
		groups = append(groups, []pdfGlyph{g})
	}

	var lines []pdfLine
	for _, group := range groups {
		sort.SliceStable(group, func(i, j int) bool { return group[i].x < group[j].x })
		var text strings.Builder
		bold, total := 0, 0
		for _, g := range group {
			text.WriteString(g.s)
			if strings.TrimSpace(g.s) != "" {
				total++
				if g.bold {
					bold++
				}
			}
		}
		if t := cleanText(text.String()); t != "" {
			lines = append(lines, pdfLine{text: t, bold: bold*2 > total})
		}
	}
	return lines
}

func cellColor(c pdfCell, boxes []pdfBox) string {
	color := ""
	for _, b := range boxes {
		if math.Abs(b.x0-c.x0) < 2 && math.Abs(b.x1-c.x1) < 2 && math.Abs(b.y0-c.y0) < 2 && math.Abs(b.y1-c.y1) < 2 {
			color = b.color
		}
	}
	return color
}
