package push

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/billypuding/watplan/internal/model"
)

var weekdays = []string{"nd", "pn", "wt", "śr", "cz", "pt", "so"}

func shortWhen(e model.Event) string {
	d, err := time.Parse("2006-01-02", e.Date)
	if err != nil {
		return e.Date + " " + e.Start
	}
	return fmt.Sprintf("%s %02d.%02d %s", weekdays[d.Weekday()], d.Day(), int(d.Month()), e.Start)
}

func label(e model.Event) string {
	if e.Type == "" {
		return e.Subject
	}
	return e.Subject + " (" + e.Type + ")"
}

func describeChanged(c model.EventChange) string {
	var parts []string
	if c.Before.Room != c.After.Room {
		parts = append(parts, fmt.Sprintf("sala %s → %s", orDash(c.Before.Room), orDash(c.After.Room)))
	}
	if c.Before.End != c.After.End {
		parts = append(parts, fmt.Sprintf("koniec %s → %s", c.Before.End, c.After.End))
	}
	if c.Before.Teacher != c.After.Teacher {
		parts = append(parts, fmt.Sprintf("prowadzący: %s", orDash(c.After.Teacher)))
	}
	if len(parts) == 0 {
		parts = append(parts, "zmienione szczegóły")
	}
	return fmt.Sprintf("%s, %s: %s", label(c.After), shortWhen(c.After), strings.Join(parts, ", "))
}

func orDash(s string) string {
	if s == "" {
		return "brak"
	}
	return s
}

func Lines(c model.Change) []string {
	var lines []string
	for _, m := range c.Moved {
		lines = append(lines, fmt.Sprintf("Przeniesione: %s, %s → %s", label(m.After), shortWhen(m.Before), shortWhen(m.After)))
	}
	for _, ch := range c.Changed {
		lines = append(lines, describeChanged(ch))
	}
	for _, e := range c.Removed {
		lines = append(lines, fmt.Sprintf("Odwołane: %s, %s", label(e), shortWhen(e)))
	}
	for _, e := range c.Added {
		lines = append(lines, fmt.Sprintf("Nowe: %s, %s", label(e), shortWhen(e)))
	}
	return lines
}

func Describe(planID, planName string, c model.Change) Payload {
	if planName == "" {
		planName = planID
	}
	lines := Lines(c)
	body := ""
	if len(lines) > 0 {
		body = lines[0]
	}
	if more := len(lines) - 1; more > 0 {
		body += fmt.Sprintf(" · i %d %s", more, changesWord(more))
	}
	return Payload{
		Title: "Zmiana w planie " + planName,
		Body:  body,
		URL:   "/app?plan=" + url.QueryEscape(planID) + "&changes=1",
		Tag:   "plan-" + planID,
	}
}

func changesWord(n int) string {
	if n == 1 {
		return "inna zmiana"
	}
	t, h := n%10, n%100
	if t >= 2 && t <= 4 && (h < 12 || h > 14) {
		return "inne zmiany"
	}
	return "innych zmian"
}
