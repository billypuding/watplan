package model

import "testing"

func ev(date, start, subject, room string) Event {
	return Event{Date: date, Start: start, End: "09:35", Subject: subject, Type: "wykład", Room: room}
}

func TestChangesDetectsMovesRoomChangesAndIgnoresPast(t *testing.T) {
	old := []Event{
		ev("2026-11-10", "08:00", "Fizyka", "19 36"),
		ev("2026-11-20", "08:00", "Matematyka", "19 36"),
		ev("2026-11-21", "08:00", "Chemia", "10 63"),
		ev("2026-11-22", "08:00", "Historia", "1 1"),
	}
	cur := []Event{
		ev("2026-11-20", "08:00", "Matematyka", "233 K"),
		ev("2026-11-24", "11:40", "Chemia", "10 63"),
		ev("2026-11-25", "08:00", "Informatyka", "4 63A"),
	}
	cur[0].Color = "#fff"

	ch := Changes(old, cur, "2026-11-19", "2026-11-19 16:00:00")
	if len(ch.Changed) != 1 || ch.Changed[0].Before.Room != "19 36" || ch.Changed[0].After.Room != "233 K" {
		t.Fatalf("zmiana sali: %+v", ch.Changed)
	}
	if len(ch.Moved) != 1 || ch.Moved[0].Before.Date != "2026-11-21" || ch.Moved[0].After.Date != "2026-11-24" {
		t.Fatalf("przeniesienie: %+v", ch.Moved)
	}
	if len(ch.Removed) != 1 || ch.Removed[0].Subject != "Historia" {
		t.Fatalf("usunięte (bez przeszłej fizyki): %+v", ch.Removed)
	}
	if len(ch.Added) != 1 || ch.Added[0].Subject != "Informatyka" {
		t.Fatalf("dodane: %+v", ch.Added)
	}
	if ch.Count() != 4 {
		t.Fatalf("Count = %d", ch.Count())
	}
}

func TestChangesIgnoresColorOnly(t *testing.T) {
	a := ev("2026-11-20", "08:00", "Matematyka", "19 36")
	b := a
	b.Color = "#123456"
	if ch := Changes([]Event{a}, []Event{b}, "2026-11-01", ""); !ch.Empty() {
		t.Fatalf("sama zmiana koloru: %+v", ch)
	}
}
