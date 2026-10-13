package model

import "fmt"

type PlanDiff struct {
	Added    []Event
	Removed  []Event
	Modified []Event
}

func Diff(oldEvents, newEvents []Event) PlanDiff {
	oldIndex := indexEvents(oldEvents)
	newIndex := indexEvents(newEvents)

	var diff PlanDiff
	for key, oldEvent := range oldIndex {
		newEvent, ok := newIndex[key]
		switch {
		case !ok:
			diff.Removed = append(diff.Removed, oldEvent)
		case oldEvent != newEvent:
			diff.Modified = append(diff.Modified, newEvent)
		}
	}
	for key, newEvent := range newIndex {
		if _, ok := oldIndex[key]; !ok {
			diff.Added = append(diff.Added, newEvent)
		}
	}

	SortEvents(diff.Added)
	SortEvents(diff.Removed)
	SortEvents(diff.Modified)
	return diff
}

func (d PlanDiff) Empty() bool {
	return len(d.Added)+len(d.Removed)+len(d.Modified) == 0
}

func indexEvents(events []Event) map[string]Event {
	index := make(map[string]Event, len(events))
	seen := make(map[string]int)

	for _, e := range events {
		base := eventKey(e)
		key := base
		if n := seen[base]; n > 0 {
			key = fmt.Sprintf("%s#%d", base, n)
		}
		seen[base]++
		index[key] = e
	}
	return index
}

func eventKey(e Event) string {
	return fmt.Sprintf("%s|%s|%s|%s", e.Date, e.Start, e.Subject, e.Type)
}
