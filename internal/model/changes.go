package model

import (
	"sort"
	"time"
)

type EventChange struct {
	Before Event `json:"before"`
	After  Event `json:"after"`
}

type Change struct {
	At      string        `json:"at"`
	Added   []Event       `json:"added,omitempty"`
	Removed []Event       `json:"removed,omitempty"`
	Moved   []EventChange `json:"moved,omitempty"`
	Changed []EventChange `json:"changed,omitempty"`
}

const maxMoveDays = 28

func (c Change) Count() int {
	return len(c.Added) + len(c.Removed) + len(c.Moved) + len(c.Changed)
}

func (c Change) Empty() bool {
	return c.Count() == 0
}

func sameContent(a, b Event) bool {
	a.Color, b.Color = "", ""
	return a == b
}

func dayDistance(a, b string) int {
	ta, errA := time.Parse("2006-01-02", a)
	tb, errB := time.Parse("2006-01-02", b)
	if errA != nil || errB != nil {
		return maxMoveDays + 1
	}
	d := int(ta.Sub(tb).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

func Changes(oldEvents, newEvents []Event, from, at string) Change {
	oldIndex := indexEvents(oldEvents)
	newIndex := indexEvents(newEvents)

	ch := Change{At: at}
	var removed, added []Event
	for key, before := range oldIndex {
		after, ok := newIndex[key]
		switch {
		case !ok:
			removed = append(removed, before)
		case !sameContent(before, after) && after.Date >= from:
			ch.Changed = append(ch.Changed, EventChange{Before: before, After: after})
		}
	}
	for key, after := range newIndex {
		if _, ok := oldIndex[key]; !ok {
			added = append(added, after)
		}
	}
	SortEvents(removed)
	SortEvents(added)

	used := make([]bool, len(added))
	for _, before := range removed {
		best, bestDist := -1, maxMoveDays+1
		for i, after := range added {
			if used[i] || after.Subject != before.Subject || after.Type != before.Type {
				continue
			}
			if d := dayDistance(before.Date, after.Date); d < bestDist {
				best, bestDist = i, d
			}
		}
		if best >= 0 {
			used[best] = true
			after := added[best]
			if before.Date >= from || after.Date >= from {
				ch.Moved = append(ch.Moved, EventChange{Before: before, After: after})
			}
			continue
		}
		if before.Date >= from {
			ch.Removed = append(ch.Removed, before)
		}
	}
	for i, after := range added {
		if !used[i] && after.Date >= from {
			ch.Added = append(ch.Added, after)
		}
	}

	byAfter := func(list []EventChange) {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i].After, list[j].After
			if a.Date != b.Date {
				return a.Date < b.Date
			}
			return a.Start < b.Start
		})
	}
	byAfter(ch.Moved)
	byAfter(ch.Changed)
	return ch
}
