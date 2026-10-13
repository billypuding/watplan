package model

import "sort"

type Event struct {
	Date    string `json:"date"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Subject string `json:"subject"`
	Type    string `json:"type"`
	Teacher string `json:"teacher"`
	Room    string `json:"room"`
	Color   string `json:"color"`
}

type Plan struct {
	Events     []Event `json:"events"`
	FetchedAt  string  `json:"fetched_at"`
	ModifiedAt string  `json:"modified_at"`
}

func SortEvents(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Type < b.Type
	})
}
