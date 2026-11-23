package model

import (
	"testing"
	"time"
)

func TestCheckInterval(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		modified time.Time
		known    bool
		want     time.Duration
	}{
		{"nieznana data zmiany", time.Time{}, false, 10 * time.Minute},
		{"zmiana godzinę temu", now.Add(-time.Hour), true, 10 * time.Minute},
		{"tuż przed dobą", now.Add(-24*time.Hour + time.Second), true, 10 * time.Minute},
		{"dokładnie doba", now.Add(-24 * time.Hour), true, time.Hour},
		{"3 dni", now.Add(-72 * time.Hour), true, time.Hour},
		{"tuż przed tygodniem", now.Add(-7*24*time.Hour + time.Second), true, time.Hour},
		{"dokładnie tydzień", now.Add(-7 * 24 * time.Hour), true, 4 * time.Hour},
		{"miesiąc", now.Add(-30 * 24 * time.Hour), true, 4 * time.Hour},
		{"data z przyszłości", now.Add(time.Hour), true, 10 * time.Minute},
	}
	for _, c := range cases {
		if got := CheckInterval(c.modified, c.known, now); got != c.want {
			t.Errorf("%s: %v, chcę %v", c.name, got, c.want)
		}
	}
}

func TestCheckIntervalsOrdered(t *testing.T) {
	for i := 1; i < len(CheckIntervals); i++ {
		prev, cur := CheckIntervals[i-1], CheckIntervals[i]
		if cur.UnchangedFor >= prev.UnchangedFor || cur.Every > prev.Every {
			t.Fatalf("progi %d i %d nie są malejące: %+v, %+v", i-1, i, prev, cur)
		}
	}
	if last := CheckIntervals[len(CheckIntervals)-1]; last.UnchangedFor != 0 {
		t.Fatalf("ostatni próg musi łapać wszystko (UnchangedFor 0), jest %v", last.UnchangedFor)
	}
}

func event(date, start, subject, typ, room string) Event {
	return Event{Date: date, Start: start, End: "09:35", Subject: subject, Type: typ, Room: room}
}

func TestDiff(t *testing.T) {
	old := []Event{
		event("2026-11-24", "08:00", "Matematyka", "wykład", "100"),
		event("2026-11-24", "09:50", "Fizyka", "ćwiczenia", "200"),
		event("2026-11-25", "08:00", "Chemia", "wykład", "300"),
	}
	cur := []Event{
		event("2026-11-24", "08:00", "Matematyka", "wykład", "101"),
		event("2026-11-25", "08:00", "Chemia", "wykład", "300"),
		event("2026-11-26", "11:40", "Fizyka", "ćwiczenia", "200"),
	}
	d := Diff(old, cur)
	if len(d.Modified) != 1 || d.Modified[0].Room != "101" {
		t.Errorf("zmienione %+v, chcę Matematykę w sali 101", d.Modified)
	}
	if len(d.Removed) != 1 || d.Removed[0].Date != "2026-11-24" || d.Removed[0].Subject != "Fizyka" {
		t.Errorf("usunięte %+v", d.Removed)
	}
	if len(d.Added) != 1 || d.Added[0].Date != "2026-11-26" {
		t.Errorf("dodane %+v", d.Added)
	}
	if d.Empty() {
		t.Error("Empty dla różnych planów")
	}
	if !Diff(old, old).Empty() || !Diff(nil, nil).Empty() {
		t.Error("ten sam plan powinien dać pusty diff")
	}
	if d := Diff(nil, old); len(d.Added) != 3 || len(d.Removed) != 0 {
		t.Errorf("nowy plan: %+v", d)
	}
}

func TestDiffHandlesDuplicateSlots(t *testing.T) {
	a := event("2026-11-24", "08:00", "Laboratorium", "lab", "1")
	b := event("2026-11-24", "08:00", "Laboratorium", "lab", "2")
	if d := Diff([]Event{a, b}, []Event{a, b}); !d.Empty() {
		t.Fatalf("dwie grupy laboratoryjne w tym samym czasie: %+v", d)
	}
	if d := Diff([]Event{a, b}, []Event{a}); len(d.Removed) != 1 || d.Removed[0].Room != "2" {
		t.Fatalf("usunięcie jednej z dwóch: %+v", d)
	}
	if d := Diff([]Event{a}, []Event{a, b}); len(d.Added) != 1 || d.Added[0].Room != "2" {
		t.Fatalf("dodanie drugiej: %+v", d)
	}
}

func TestDiffIsSorted(t *testing.T) {
	cur := []Event{
		event("2026-11-26", "08:00", "B", "", ""),
		event("2026-11-24", "11:40", "A", "", ""),
		event("2026-11-24", "08:00", "C", "", ""),
	}
	d := Diff(nil, cur)
	if d.Added[0].Subject != "C" || d.Added[1].Subject != "A" || d.Added[2].Subject != "B" {
		t.Fatalf("kolejność %+v", d.Added)
	}
}

func TestSortEvents(t *testing.T) {
	events := []Event{
		{Date: "2026-11-24", Start: "08:00", Subject: "B", Type: "wykład"},
		{Date: "2026-11-24", Start: "08:00", Subject: "A", Type: "wykład", Room: "druga"},
		{Date: "2026-11-24", Start: "08:00", Subject: "A", Type: "ćwiczenia"},
		{Date: "2026-11-23", Start: "16:00", Subject: "Z"},
		{Date: "2026-11-24", Start: "07:30", Subject: "Z"},
		{Date: "2026-11-24", Start: "08:00", Subject: "A", Type: "wykład", Room: "pierwsza"},
	}
	SortEvents(events)
	want := []string{
		"2026-11-23 16:00 Z  ",
		"2026-11-24 07:30 Z  ",
		"2026-11-24 08:00 A wykład druga",
		"2026-11-24 08:00 A wykład pierwsza",
		"2026-11-24 08:00 A ćwiczenia ",
		"2026-11-24 08:00 B wykład ",
	}
	for i, e := range events {
		if got := e.Date + " " + e.Start + " " + e.Subject + " " + e.Type + " " + e.Room; got != want[i] {
			t.Fatalf("pozycja %d: %q, chcę %q", i, got, want[i])
		}
	}
}

func TestLastChecked(t *testing.T) {
	if got := (Plan{FetchedAt: "2026-11-20 10:00:00"}).LastChecked(); got != "2026-11-20 10:00:00" {
		t.Fatalf("bez checked_at: %q", got)
	}
	if got := (Plan{FetchedAt: "2026-11-20 10:00:00", CheckedAt: "2026-11-23 08:00:00"}).LastChecked(); got != "2026-11-23 08:00:00" {
		t.Fatalf("z checked_at: %q", got)
	}
}
