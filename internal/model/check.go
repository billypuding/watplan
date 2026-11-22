package model

import "time"

var CheckIntervals = []struct {
	UnchangedFor time.Duration
	Every        time.Duration
}{
	{7 * 24 * time.Hour, 4 * time.Hour},
	{24 * time.Hour, time.Hour},
	{0, 10 * time.Minute},
}

func CheckInterval(modified time.Time, known bool, now time.Time) time.Duration {
	if known {
		age := now.Sub(modified)
		for _, v := range CheckIntervals {
			if age >= v.UnchangedFor {
				return v.Every
			}
		}
	}
	return CheckIntervals[len(CheckIntervals)-1].Every
}
