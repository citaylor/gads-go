package gads

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// duration is a parsed Date::Manip delta.
type duration struct {
	years, months, weeks, days, hours, minutes, seconds int
}

func (d duration) addTo(t time.Time) time.Time {
	t = t.AddDate(d.years, d.months, d.weeks*7+d.days)
	return t.Add(time.Duration(d.hours)*time.Hour + time.Duration(d.minutes)*time.Minute + time.Duration(d.seconds)*time.Second)
}

var reDurTerm = regexp.MustCompile(`(?i)([+-]?)\s*(\d+)\s*(years?|yrs?|y|months?|mons?|weeks?|wks?|w|days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b`)

// parseDuration implements the subset of DateTime::Format::DateManip
// parse_duration used by GADS bulk updates (e.g. "+1 day", "2 weeks").
func parseDuration(s string) (duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return duration{}, false
	}
	ago := false
	ls := strings.ToLower(s)
	if strings.HasSuffix(ls, " ago") {
		ago = true
		s = strings.TrimSpace(s[:len(s)-4])
	}
	if strings.HasPrefix(strings.ToLower(s), "in ") {
		s = strings.TrimSpace(s[3:])
	}
	matches := reDurTerm.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return duration{}, false
	}
	// The whole string must be made up of terms
	rest := reDurTerm.ReplaceAllString(s, "")
	if strings.Trim(rest, " ,and") != "" {
		return duration{}, false
	}
	var d duration
	sign := 1
	for _, m := range matches {
		if s[m[2]:m[3]] == "-" {
			sign = -1
		} else if s[m[2]:m[3]] == "+" {
			sign = 1
		}
		n, _ := strconv.Atoi(s[m[4]:m[5]])
		n *= sign
		if ago {
			n = -n
		}
		unit := strings.ToLower(s[m[6]:m[7]])
		switch {
		case strings.HasPrefix(unit, "y"):
			d.years += n
		case strings.HasPrefix(unit, "mon"):
			d.months += n
		case strings.HasPrefix(unit, "w"):
			d.weeks += n
		case strings.HasPrefix(unit, "d"):
			d.days += n
		case strings.HasPrefix(unit, "h"):
			d.hours += n
		case strings.HasPrefix(unit, "mi") || unit == "m":
			d.minutes += n
		case strings.HasPrefix(unit, "s"):
			d.seconds += n
		}
	}
	return d, true
}
