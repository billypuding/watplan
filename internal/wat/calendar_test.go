package wat

import (
	"testing"
	"time"
)

func TestFindDaysRollsYearAtBoundary(t *testing.T) {
	grid := [][]Cell{
		{
			{Text: "pon.", Row: 0, Col: 0},
			{Text: "29 XII", Row: 0, Col: 2},
			{Text: "05 I", Row: 0, Col: 3},
		},
	}

	blocks, err := findDays(grid, 5, 2025)
	if err != nil {
		t.Fatalf("findDays: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("liczba bloków = %d, chcę 1", len(blocks))
	}

	want := map[int]time.Time{
		2: time.Date(2025, time.December, 29, 0, 0, 0, 0, time.UTC),
		3: time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC),
	}
	for col, date := range want {
		got, ok := blocks[0].Dates[col]
		if !ok {
			t.Errorf("brak daty w kolumnie %d", col)
			continue
		}
		if !got.Equal(date) {
			t.Errorf("kolumna %d = %s, chcę %s", col, got.Format(dateLayout), date.Format(dateLayout))
		}
	}
}

func TestFindDaysKeepsOnlyRealDayHeaders(t *testing.T) {
	grid := [][]Cell{
		{
			{Text: "pon.", Row: 0, Col: 0},
			{Text: "06 X", Row: 0, Col: 2},
		},
	}
	markers := []string{"Pms", "REZ", "I", "ZbMw13 71Lew.R", "Swbw6/ 23AMu", "Pdp", "I"}
	for i, marker := range markers {
		row := i + 1
		grid = append(grid, []Cell{
			{Text: "pon.", Row: row, Col: 0},
			{Text: slotLabels[i], Row: row, Col: 1},
			{Text: marker, Row: row, Col: 2},
		})
	}
	grid = append(grid,
		[]Cell{
			{Text: "wt.", Row: 8, Col: 0},
			{Text: "07 X", Row: 8, Col: 2},
		},
		[]Cell{
			{Text: "wt.", Row: 9, Col: 0},
			{Text: "1-2", Row: 9, Col: 1},
			{Text: "Pdp", Row: 9, Col: 2},
		},
	)

	blocks, err := findDays(grid, 4, 2025)
	if err != nil {
		t.Fatalf("findDays: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("liczba bloków = %d, chcę 2", len(blocks))
	}
	if blocks[0].HeaderRow != 0 || blocks[1].HeaderRow != 8 {
		t.Fatalf("nagłówki bloków = %d, %d, chcę 0, 8", blocks[0].HeaderRow, blocks[1].HeaderRow)
	}

	if b, ok := blockForRow(blocks, 1); !ok || b.HeaderRow != 0 {
		t.Errorf("wiersz 1 -> blok %d (ok=%v), chcę nagłówek 0", b.HeaderRow, ok)
	}
	if b, ok := blockForRow(blocks, 9); !ok || b.HeaderRow != 8 {
		t.Errorf("wiersz 9 -> blok %d (ok=%v), chcę nagłówek 8", b.HeaderRow, ok)
	}
}

func TestBlockRangeStopsAtNextDay(t *testing.T) {
	grid := [][]Cell{
		{
			{Text: "pon.", Row: 0, Col: 0},
			{Text: "06 X", Row: 0, Col: 2},
		},
		{
			{Text: "pon.", Row: 1, Col: 0},
			{Text: "1-2", Row: 1, Col: 1},
			{Text: "Mat", Row: 1, Col: 2},
		},
		{
			{Text: "wt.", Row: 2, Col: 0},
			{Text: "07 X", Row: 2, Col: 3},
		},
		{
			{Text: "wt.", Row: 3, Col: 0},
			{Text: "1-2", Row: 3, Col: 1},
			{Text: "Fiz", Row: 3, Col: 3},
		},
	}

	blocks, err := findDays(grid, 5, 2025)
	if err != nil {
		t.Fatalf("findDays: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("liczba bloków = %d, chcę 2", len(blocks))
	}

	if b, ok := blockForRow(blocks, 2); ok {
		t.Errorf("wiersz 2 to nagłówek dnia, dostałem blok %d", b.HeaderRow)
	}
	if b, ok := blockForRow(blocks, 1); !ok || b.HeaderRow != 0 {
		t.Errorf("wiersz 1 -> blok %d (ok=%v), chcę nagłówek 0", b.HeaderRow, ok)
	}
	if b, ok := blockForRow(blocks, 3); !ok || b.HeaderRow != 2 {
		t.Errorf("wiersz 3 -> blok %d (ok=%v), chcę nagłówek 2", b.HeaderRow, ok)
	}
}

func TestFindDaysDropsRowWithoutDates(t *testing.T) {
	grid := [][]Cell{
		{
			{Text: "czw.", Row: 10, Col: 0},
			{Text: "Pms", Row: 10, Col: 2},
			{Text: "REZ", Row: 10, Col: 3},
			{Text: "ZbMw13 71Lew.R", Row: 10, Col: 4},
		},
	}

	blocks, err := findDays(grid, 6, 2025)
	if err != nil {
		t.Fatalf("findDays: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("liczba bloków = %d, chcę 0", len(blocks))
	}
}

func TestLooksLikeDate(t *testing.T) {
	cases := map[string]bool{
		"05 X":                             true,
		"13-14":                            false,
		"Pms":                              false,
		"REZ":                              false,
		"I":                                false,
		"ZbMw13 71Lew.R":                   false,
		"Swbw6/ 23AMu":                     false,
		"Aspekty praktyczne projektowania": false,
	}
	for in, want := range cases {
		if got := looksLikeDate(in); got != want {
			t.Errorf("looksLikeDate(%q) = %v, chcę %v", in, got, want)
		}
	}
}

func TestCellTimes(t *testing.T) {
	block := dayBlock{Weekday: time.Monday, HeaderRow: 0}

	cases := []struct {
		name      string
		cell      Cell
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{
			name:      "pojedynczy slot",
			cell:      Cell{Row: 1, Col: 2, RowSpan: 1},
			wantStart: "08:00",
			wantEnd:   "09:35",
		},
		{
			name:      "dwa sloty",
			cell:      Cell{Row: 2, Col: 2, RowSpan: 2},
			wantStart: "09:50",
			wantEnd:   "13:15",
		},
		{
			name:    "wiersz przed siatką",
			cell:    Cell{Row: 0, Col: 2, RowSpan: 1},
			wantErr: true,
		},
		{
			name:    "wiersz za siatką",
			cell:    Cell{Row: 8, Col: 2, RowSpan: 1},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, err := cellTimes(c.cell, block)
			if c.wantErr {
				if err == nil {
					t.Fatalf("chcę błąd, dostałem (%q, %q)", start, end)
				}
				return
			}
			if err != nil {
				t.Fatalf("cellTimes: %v", err)
			}
			if start != c.wantStart || end != c.wantEnd {
				t.Errorf("cellTimes = (%q, %q), chcę (%q, %q)", start, end, c.wantStart, c.wantEnd)
			}
		})
	}
}
