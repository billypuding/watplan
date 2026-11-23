package server

import (
	"runtime/debug"
	"time"
)

func buildVersion(assets string) string {
	short := assets
	if len(short) > 7 {
		short = short[:7]
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return short
	}
	var rev, at string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			at = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) < 7 {
		return short
	}
	v := rev[:7]
	if dirty {
		v += "+"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		v += " (" + t.In(warsawLoc).Format("02.01.2006 15:04") + ")"
	}
	return v
}
