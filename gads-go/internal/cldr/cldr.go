// Package cldr implements the subset of CLDR date pattern formatting
// (DateTime::format_cldr) and parsing (DateTime::Format::CLDR) used by
// GADS, using the default en-US locale.
package cldr

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var monthsWide = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
var monthsAbbr = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
var monthsNarrow = []string{"J", "F", "M", "A", "M", "J", "J", "A", "S", "O", "N", "D"}

// DateTime day_of_week: 1 = Monday ... 7 = Sunday
var daysWide = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
var daysAbbr = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
var daysNarrow = []string{"M", "T", "W", "T", "F", "S", "S"}

type token struct {
	literal string
	command byte
	length  int
}

func tokenize(pattern string) []token {
	var out []token
	i := 0
	for i < len(pattern) {
		c := pattern[i]
		if c == '\'' {
			// quoted text
			j := i + 1
			var sb strings.Builder
			for j < len(pattern) {
				if pattern[j] == '\'' {
					if j+1 < len(pattern) && pattern[j+1] == '\'' {
						sb.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				sb.WriteByte(pattern[j])
				j++
			}
			out = append(out, token{literal: sb.String()})
			i = j + 1
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			j := i
			for j < len(pattern) && pattern[j] == c {
				j++
			}
			out = append(out, token{command: c, length: j - i})
			i = j
			continue
		}
		// literal rune
		r := []rune(pattern[i:])[0]
		out = append(out, token{literal: string(r)})
		i += len(string(r))
	}
	return out
}

// pad is sprintf "%0*d" (the width includes the sign, as in Perl)
func pad(n, width int) string { return fmt.Sprintf("%0*d", width, n) }

// dow returns DateTime day_of_week (1=Mon..7=Sun)
func dow(t time.Time) int {
	w := int(t.Weekday())
	if w == 0 {
		return 7
	}
	return w
}

// The rule of a run of a pattern letter in DateTime's format_cldr: the
// first rule of the letter whose regular expression matches in the run (of
// n letters) applies, min..max being the repetitions it matches (max 0: any
// number); value formats it, with width the number of letters matched.
type formatRule struct {
	min, max int
	value    func(t time.Time, width int) string
}

func zeroPadded(v func(time.Time) int) func(time.Time, int) string {
	return func(t time.Time, width int) string { return pad(v(t), width) }
}

func plain(v func(time.Time) int) func(time.Time, int) string {
	return func(t time.Time, _ int) string { return strconv.Itoa(v(t)) }
}

func text(v func(time.Time) string) func(time.Time, int) string {
	return func(t time.Time, _ int) string { return v(t) }
}

func era(names [2]string) func(time.Time, int) string {
	return func(t time.Time, _ int) string {
		if t.Year() > 0 {
			return names[1]
		}
		return names[0]
	}
}

func month(t time.Time) int    { return int(t.Month()) }
func quarter(t time.Time) int  { return (int(t.Month())-1)/3 + 1 }
func weekNum(t time.Time) int  { _, w := t.ISOWeek(); return w }
func weekYear(t time.Time) int { y, _ := t.ISOWeek(); return y }
func hour12(t time.Time) int {
	if h := t.Hour() % 12; h != 0 {
		return h
	}
	return 12
}

// localDow is DateTime local_day_of_week (en-US: weeks start on Sunday)
func localDow(t time.Time) int { return 1 + (dow(t)-7+7)%7 }

// mjd is DateTime mjd, the modified Julian day with its fraction, as %d
// formats it (truncated towards zero)
func mjd(t time.Time) int {
	u := t.Unix()
	days := u / 86400
	if u%86400 < 0 {
		days--
	}
	secs := u - days*86400
	return int(math.Trunc(float64(days+40587) + float64(secs)/86400))
}

var quarterNames = []string{"1st quarter", "2nd quarter", "3rd quarter", "4th quarter"}

func zoneName(t time.Time) string { name, _ := t.Zone(); return name }

// offset is DateTime::TimeZone offset_as_string
func offset(t time.Time, sep string) string {
	_, off := t.Zone()
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	return fmt.Sprintf("%s%02d%s%02d", sign, off/3600, sep, (off%3600)/60)
}

// formatRules is the table of DateTime format_cldr (DateTime.pm @patterns)
// for the en-US locale
var formatRules = map[byte][]formatRule{
	'G': {{5, 0, era([2]string{"B", "A"})}, {4, 4, era([2]string{"Before Christ", "Anno Domini"})}, {1, 3, era([2]string{"BC", "AD"})}},
	'y': {
		{3, 5, zeroPadded(func(t time.Time) int { return t.Year() })},
		// yy: exactly the last 2 digits
		{2, 2, func(t time.Time, _ int) string {
			y := t.Year()
			y2 := y
			if s := strconv.Itoa(y); len(s) > 2 {
				y2, _ = strconv.Atoi(s[len(s)-2:])
				if y < 0 {
					y2 = -y2
				}
			}
			return pad(y2, 2)
		}},
		{1, 1, plain(func(t time.Time) int { return t.Year() })},
	},
	'u': {{1, 0, zeroPadded(func(t time.Time) int { return t.Year() })}},
	'Y': {{1, 0, zeroPadded(weekYear)}},
	'Q': {{4, 4, text(func(t time.Time) string { return quarterNames[quarter(t)-1] })},
		{3, 3, text(func(t time.Time) string { return "Q" + strconv.Itoa(quarter(t)) })},
		{1, 2, zeroPadded(quarter)}},
	'M': {{5, 0, text(func(t time.Time) string { return monthsNarrow[month(t)-1] })},
		{4, 4, text(func(t time.Time) string { return monthsWide[month(t)-1] })},
		{3, 3, text(func(t time.Time) string { return monthsAbbr[month(t)-1] })},
		{1, 2, zeroPadded(month)}},
	'w': {{1, 2, zeroPadded(weekNum)}},
	'W': {{1, 1, plain(weekOfMonth)}},
	'd': {{1, 2, zeroPadded(func(t time.Time) int { return t.Day() })}},
	'D': {{1, 3, zeroPadded(func(t time.Time) int { return t.YearDay() })}},
	'F': {{1, 1, plain(func(t time.Time) int { return (t.Day()-1)/7 + 1 })}},
	'g': {{1, 0, zeroPadded(mjd)}},
	'E': {{5, 0, text(func(t time.Time) string { return daysNarrow[dow(t)-1] })},
		{4, 4, text(func(t time.Time) string { return daysWide[dow(t)-1] })},
		{1, 3, text(func(t time.Time) string { return daysAbbr[dow(t)-1] })}},
	'e': {{5, 0, text(func(t time.Time) string { return daysNarrow[dow(t)-1] })},
		{4, 4, text(func(t time.Time) string { return daysWide[dow(t)-1] })},
		{3, 3, text(func(t time.Time) string { return daysAbbr[dow(t)-1] })},
		{1, 2, zeroPadded(localDow)}},
	// (the stand-alone forms, the same in en-US; cc is day_of_week, not
	// the local day of the week)
	'c': {{5, 0, text(func(t time.Time) string { return daysNarrow[dow(t)-1] })},
		{4, 4, text(func(t time.Time) string { return daysWide[dow(t)-1] })},
		{3, 3, text(func(t time.Time) string { return daysAbbr[dow(t)-1] })},
		{1, 2, zeroPadded(dow)}},
	'a': {{1, 1, text(func(t time.Time) string {
		if t.Hour() < 12 {
			return "AM"
		}
		return "PM"
	})}},
	'h': {{1, 2, zeroPadded(hour12)}},
	'H': {{1, 2, zeroPadded(func(t time.Time) int { return t.Hour() })}},
	'K': {{1, 2, zeroPadded(func(t time.Time) int { return t.Hour() % 12 })}},
	'k': {{1, 2, zeroPadded(func(t time.Time) int {
		if t.Hour() == 0 {
			return 24
		}
		return t.Hour()
	})}},
	// (en-US does not prefer 24 hour time)
	'j': {{1, 2, zeroPadded(hour12)}},
	'm': {{1, 2, zeroPadded(func(t time.Time) int { return t.Minute() })}},
	's': {{1, 2, zeroPadded(func(t time.Time) int { return t.Second() })}},
	'S': {{1, 0, func(t time.Time, width int) string {
		ns := fmt.Sprintf("%09d", t.Nanosecond())
		if width <= 9 {
			return ns[:width]
		}
		return ns + strings.Repeat("0", width-9)
	}}},
	'A': {{1, 0, plain(func(t time.Time) int {
		return ((t.Hour()*60+t.Minute())*60+t.Second())*1000 + t.Nanosecond()/1e6
	})}},
	'z': {{4, 4, text(func(t time.Time) string { return t.Location().String() })}, {1, 3, text(zoneName)}},
	'Z': {{5, 5, text(func(t time.Time) string { return offset(t, ":") })},
		{4, 4, text(func(t time.Time) string { return zoneName(t) + offset(t, "") })},
		{1, 3, text(func(t time.Time) string { return offset(t, "") })}},
	'v': {{4, 4, text(func(t time.Time) string { return t.Location().String() })}, {1, 3, text(zoneName)}},
	'V': {{4, 4, text(func(t time.Time) string { return t.Location().String() })}, {1, 3, text(zoneName)}},
}

func init() {
	// The stand-alone forms of the quarter and month are those of the
	// format forms in en-US
	formatRules['q'] = formatRules['Q']
	formatRules['L'] = formatRules['M']
}

// The quoted text of a pattern: a quote, then anything but a quote or two
// quotes, then a quote
var reQuoted = regexp.MustCompile(`^'((?:[^']|'')*)'`)

// Format formats t (already in the desired time zone) with a CLDR pattern
// as DateTime::format_cldr does.
func Format(t time.Time, pattern string) string {
	var sb strings.Builder
	for i := 0; i < len(pattern); {
		c := pattern[i]
		if m := reQuoted.FindStringSubmatchIndex(pattern[i:]); m != nil {
			sb.WriteString(pattern[i+m[2] : i+m[3]])
			i += m[1]
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			j := i
			for j < len(pattern) && pattern[j] == c {
				j++
			}
			sb.WriteString(formatRun(t, c, j-i))
			i = j
			continue
		}
		sb.WriteByte(c)
		i++
	}
	// (as format_cldr, on the whole result)
	return strings.ReplaceAll(sb.String(), "''", "'")
}

// formatRun formats a run of n of the letter c
func formatRun(t time.Time, c byte, n int) string {
	for _, r := range formatRules[c] {
		if n < r.min {
			continue
		}
		width := n
		if r.max > 0 && width > r.max {
			width = r.max
		}
		return r.value(t, width)
	}
	// an unknown letter is left as it is
	return strings.Repeat(string(c), n)
}

// weekOfMonth is DateTime week_of_month: the week (starting on Monday) of
// the month's Thursdays
func weekOfMonth(t time.Time) int {
	thu := t.Day() + 4 - dow(t)
	return (thu + 6) / 7
}

// ---------------------------------------------------------------------
// Parsing (DateTime::Format::CLDR)
// ---------------------------------------------------------------------

type parsePart struct {
	re      *regexp.Regexp
	command byte
	index   int
	list    []string
}

var parserRe = map[string]string{
	"y1": `(-?\d{1,4})`,
	"y2": `(-?\d{2})`,
	"y3": `(-?\d{3,4})`,
	"y4": `(-?\d{4})`,
	"y5": `(-?\d{5})`,
	"Y1": `(-?\d{1,4})`,
	"u1": `(-?\d{1,4})`,
	"Q1": `([1-4])`,
	"q1": `([1-4])`,
	"M1": `(1[0-2]|0?[1-9])`,
	"L1": `(1[0-2]|0?[1-9])`,
	"w1": `(5[0-3]|[1-4]\d|0?[1-9])`,
	"W1": `(\d)`,
	"d1": `(3[01]|[12]\d|0?[1-9])`,
	"D1": `([1-3]\d\d|0?[1-9]\d|(?:00)?[1-9])`,
	"F1": `(\d)`,
	"e1": `([1-7])`,
	"c1": `([1-7])`,
	"h1": `(1[0-2]|0?[1-9])`,
	"H1": `(00|2[0-4]|1\d|0?\d)`,
	"K1": `(00|1[01]|0?\d)`,
	"k1": `(2[0-4]|1\d|0?[1-9])`,
	"m1": `([0-5]?\d)`,
	"s1": `(6[01]|[0-5]?\d)`,
	"S1": `(\d+)`,
	"Z5": `([+-]\d\d:\d\d)`,
}

var parserList = map[string][]string{
	"M3": monthsAbbr, "M4": monthsWide, "M5": monthsNarrow,
	"L3": monthsAbbr, "L4": monthsWide, "L5": monthsNarrow,
	"E1": daysAbbr, "E4": daysWide, "E5": daysNarrow,
	"e3": daysAbbr, "e4": daysWide, "e5": daysNarrow,
	"c3": daysAbbr, "c4": daysWide, "c5": daysNarrow,
	"a1": {"AM", "PM"},
	"G1": {"BC", "AD"}, "G4": {"Before Christ", "Anno Domini"}, "G5": {"B", "A"},
	"Q3": {"Q1", "Q2", "Q3", "Q4"}, "Q4": {"1st quarter", "2nd quarter", "3rd quarter", "4th quarter"},
	"q3": {"Q1", "Q2", "Q3", "Q4"}, "q4": {"1st quarter", "2nd quarter", "3rd quarter", "4th quarter"},
}

func quoteString(s string) string {
	var sb strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !inSpace {
				sb.WriteString(`\s+`)
			}
			inSpace = true
			continue
		}
		inSpace = false
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127 {
			sb.WriteRune(r)
		} else {
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return sb.String()
}

func quoteList(list []string) string {
	sorted := append([]string(nil), list...)
	// sort by length descending (stable to keep original order otherwise)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && len(sorted[j]) > len(sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	parts := make([]string, len(sorted))
	for i, s := range sorted {
		parts[i] = quoteString(s)
	}
	return "(" + strings.Join(parts, "|") + ")"
}

var patternCache = map[string][]parsePart{}

func buildPattern(pattern string) ([]parsePart, error) {
	if p, ok := patternCache[pattern]; ok {
		return p, nil
	}
	var parts []parsePart
	for _, tk := range tokenize(pattern) {
		if tk.command == 0 {
			// (text that is false in Perl, empty or "0", is skipped)
			if tk.literal == "" || tk.literal == "0" {
				continue
			}
			parts = append(parts, parsePart{re: regexp.MustCompile(`(?i)^\s*` + quoteString(tk.literal))})
			continue
		}
		cmd := tk.command
		if cmd == 'j' {
			cmd = 'h'
		}
		found := false
		for count := tk.length; count > 0; count-- {
			key := string(cmd) + strconv.Itoa(count)
			if re, ok := parserRe[key]; ok {
				parts = append(parts, parsePart{re: regexp.MustCompile(`(?i)^\s*` + re), command: cmd, index: count})
				found = true
				break
			}
			if list, ok := parserList[key]; ok {
				parts = append(parts, parsePart{re: regexp.MustCompile(`(?i)^\s*` + quoteList(list)), command: cmd, index: count, list: list})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Broken pattern: %c %d", cmd, tk.length)
		}
	}
	patternCache[pattern] = parts
	return parts, nil
}

// Parse parses value with the CLDR pattern, returning a time in UTC
// (GADS treats the resulting floating time as UTC), or in the time zone of
// the value with ZZZZZ. ok is false where DateTime::Format::CLDR would
// return undef.
func Parse(value, pattern string) (time.Time, bool) {
	parts, err := buildPattern(pattern)
	if err != nil {
		return time.Time{}, false
	}
	s := value
	year, month, day := 0, 0, 0
	haveYear, haveMonth, haveDay := false, false, false
	hour, minute, second, nano := 0, 0, 0, 0
	var hour12, hour24, ampm, era *int
	checks := map[string]int{}
	loc := time.UTC
	for _, p := range parts {
		m := p.re.FindStringSubmatchIndex(s)
		if m == nil {
			return time.Time{}, false
		}
		capture := ""
		if len(m) >= 4 && m[2] >= 0 {
			capture = s[m[2]:m[3]]
		}
		s = s[m[1]:]
		if p.command == 0 {
			continue
		}
		if p.command == 'Z' {
			// ZZZZZ: the time zone of the value, an offset such as +01:00
			sign := 1
			if capture[0] == '-' {
				sign = -1
			}
			hh, _ := strconv.Atoi(capture[1:3])
			mm, _ := strconv.Atoi(capture[4:6])
			loc = time.FixedZone(capture, sign*(hh*3600+mm*60))
			continue
		}
		var num int
		if p.list != nil {
			idx := 0
			count := 1
			ambiguous := false
			for _, e := range p.list {
				if strings.EqualFold(e, capture) {
					if idx != 0 {
						ambiguous = true
					}
					idx = count
				}
				count++
			}
			if ambiguous {
				continue
			}
			num = idx
		} else {
			n, err := strconv.Atoi(capture)
			if err != nil {
				return time.Time{}, false
			}
			num = n
		}
		switch p.command {
		case 'G':
			// (the index of the era in the list, from 1: so DateTime::
			// Format::CLDR's check for era 0, BC, never matches, and BC
			// years are not negated)
			v := num
			era = &v
		case 'y':
			if p.index == 2 {
				if num >= 70 {
					year = num + 1900
				} else {
					year = num + 2000
				}
			} else {
				year = num
			}
			haveYear = true
		case 'Y', 'u':
			// (DateTime::Format::CLDR matches them, but has no rule for
			// them: "Unknown pattern", and the parse fails)
			return time.Time{}, false
		case 'Q', 'q':
			checks["quarter"] = num
		case 'M', 'L':
			month = num
			haveMonth = true
		case 'w':
			checks["week_number"] = num
		case 'W':
			checks["week_of_month"] = num
		case 'd':
			day = num
			haveDay = true
		case 'D':
			checks["day_of_year"] = num
		case 'e':
			if p.index == 1 {
				// (the parser's default locale is en, whose weeks start
				// on Monday)
				fdow := 1
				num -= (8 - fdow)
				if num < 1 {
					num += 7
				}
			}
			checks["day_of_week"] = num
		case 'E', 'c':
			checks["day_of_week"] = num
		case 'F':
			checks["weekday_of_month"] = num
		case 'a':
			v := num
			ampm = &v
		case 'h':
			if num == 12 {
				num = 0
			}
			v := num
			hour12 = &v
		case 'K':
			v := num
			hour12 = &v
		case 'H':
			hour = num
		case 'k':
			v := num
			hour24 = &v
		case 'm':
			minute = num
		case 's':
			second = num
		case 'S':
			f, _ := strconv.ParseFloat("0."+capture, 64)
			nano = int(f * 1e9)
		}
	}
	if s != "" {
		return time.Time{}, false
	}
	if hour12 != nil && ampm != nil {
		hour = *hour12
		if *ampm == 2 && hour < 12 {
			hour += 12
		}
	}
	if hour24 != nil {
		hour = *hour24
		if hour == 24 {
			hour = 0
		}
	}
	dayAdd := false
	if hour == 24 {
		if minute == 0 && second == 0 && nano == 0 {
			hour = 0
			dayAdd = true
		} else {
			return time.Time{}, false
		}
	}
	if era != nil && *era == 0 && haveYear {
		year = -year
	}
	if !(haveYear && haveMonth && haveDay) {
		if day == 0 {
			day = 1
		}
		if month == 0 {
			month = 1
		}
		if year == 0 {
			year = 1
		}
	}
	// DateTime->new validation
	if month < 1 || month > 12 || day < 1 || day > daysIn(year, month) ||
		hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 61 {
		return time.Time{}, false
	}
	if second > 59 {
		// leap seconds only valid in UTC at end of day - treat as invalid
		return time.Time{}, false
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, nano, loc)
	if dayAdd {
		t = t.AddDate(0, 0, 1)
	}
	for check, v := range checks {
		var got int
		switch check {
		case "quarter":
			got = (int(t.Month())-1)/3 + 1
		case "week_number":
			_, got = t.ISOWeek()
		case "week_of_month":
			got = weekOfMonth(t)
		case "day_of_year":
			got = t.YearDay()
		case "day_of_week":
			got = dow(t)
		case "weekday_of_month":
			got = (t.Day()-1)/7 + 1
		}
		if got != v {
			return time.Time{}, false
		}
	}
	return t, true
}

func daysIn(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
