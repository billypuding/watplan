package wat

import (
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type pos struct{ r, c int }

type Cell struct {
	Text    string
	ColSpan int
	RowSpan int
	Row     int
	Col     int
	Node    *goquery.Selection
}

type cellField struct {
	text string
	bold bool
}

func buildGrid(doc *goquery.Document) [][]Cell {
	occupied := make(map[pos]bool)
	var grid [][]Cell

	doc.Find("table tr").Each(func(i int, tr *goquery.Selection) {
		col := 0
		var row []Cell

		tr.ChildrenFiltered("td, th").Each(func(_ int, td *goquery.Selection) {
			for occupied[pos{i, col}] {
				col++
			}

			colspan := spanAttr(td, "colspan")
			rowspan := spanAttr(td, "rowspan")

			row = append(row, Cell{
				Text:    strings.TrimSpace(td.Text()),
				ColSpan: colspan,
				RowSpan: rowspan,
				Row:     i,
				Col:     col,
				Node:    td,
			})

			for r := 0; r < rowspan; r++ {
				for c := 0; c < colspan; c++ {
					occupied[pos{i + r, col + c}] = true
				}
			}
			col += colspan
		})

		grid = append(grid, row)
	})

	return grid
}

func spanAttr(s *goquery.Selection, name string) int {
	val, ok := s.Attr(name)
	if !ok {
		return 1
	}
	num, err := strconv.Atoi(val)
	if err != nil || num < 1 {
		return 1
	}
	return num
}

func gridWidth(grid [][]Cell) int {
	width := 0
	for _, row := range grid {
		for _, cell := range row {
			if end := cell.Col + cell.ColSpan; end > width {
				width = end
			}
		}
	}
	return width
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(s), " ")
}

func collectFields(sel *goquery.Selection) []cellField {
	var out []cellField
	var walk func(n *html.Node, bold bool)
	walk = func(n *html.Node, bold bool) {
		switch n.Type {
		case html.TextNode:
			if t := cleanText(n.Data); t != "" {
				out = append(out, cellField{text: t, bold: bold})
			}
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style":
				return
			case "b", "strong":
				bold = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, bold)
		}
	}
	for _, n := range sel.Nodes {
		walk(n, false)
	}
	return out
}
