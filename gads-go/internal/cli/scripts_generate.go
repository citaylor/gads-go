package cli

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ctrlo/gads-go/internal/cldr"
	"github.com/ctrlo/gads-go/internal/gads"
)

func init() {
	Commands["generate.pl"] = Generate
}

// csvField is a field of Text::CSV (its defaults): quoted if it has a
// separator, quote, space or line break, with the quotes doubled
func csvField(s string) string {
	if strings.ContainsAny(s, ",\" \t\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func csvLine(fields []string) string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = csvField(f)
	}
	return strings.Join(out, ",") + "\n"
}

// randomValue is Column->random: a random value of the field, or "" if
// it has none
func randomValue(e *gads.Engine, col *gads.Column) (string, bool) {
	switch col.Type {
	case "enum", "tree":
		var vals []string
		for _, v := range col.Enumvals() {
			if col.Type == "tree" && truthy(v["deleted"]) {
				continue
			}
			if s := str(v["value"]); !perlFalse(s) || col.Type == "enum" {
				vals = append(vals, s)
			}
		}
		if len(vals) == 0 {
			// A tree with no values loops for ever in GADS
			return "", true
		}
		return vals[rand.Intn(len(vals))], true
	case "curval":
		var ids []int64
		if ref := col.RefersToInstanceID(); ref != nil {
			ids = e.DB.QueryInt64s(`SELECT "id" FROM "current" WHERE "instance_id" = ? AND "deleted" IS NULL AND "draftuser_id" IS NULL ORDER BY "id"`, *ref)
		}
		if len(ids) == 0 {
			return "", true
		}
		return strconv.FormatInt(ids[rand.Intn(len(ids))], 10), true
	case "person":
		people := col.People()
		if len(people) == 0 {
			Die(`Can't call method "value" on an undefined value`)
		}
		return people[rand.Intn(len(people))].Value, true
	}
	return "", false
}

// Generate is bin/generate.pl: writes new.csv with 1000 rows of random
// values of the fields of a table, for importing
func Generate(env *Env) {
	var instanceID, siteID string
	if !env.GetOptions(
		Option{Name: "instance-id", Value: true, String: &instanceID},
		Option{Name: "site-id", Value: true, String: &siteID},
	) {
		env.Exit(0)
	}
	if perlFalse(instanceID) {
		Die("Need instance ID with --instance-id")
	}
	if perlFalse(siteID) {
		Die("Need site ID with --site-id")
	}
	e := env.E
	e.SiteID, _ = strconv.ParseInt(siteID, 10, 64)
	iid, _ := strconv.ParseInt(instanceID, 10, 64)
	layout := e.NewLayout(iid, nil)
	var header []string
	var columns []*gads.Column
	for _, col := range layout.All(gads.AllOptions{}) {
		if col.Type == "file" || !col.Userinput() {
			continue
		}
		header = append(header, col.Name)
		columns = append(columns, col)
	}
	start := time.Date(2013, 10, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 8, 23, 0, 0, 0, 0, time.UTC)
	randomDate := func() time.Time {
		return start.Add(time.Duration(rand.Int63n(int64(end.Sub(start)))))
	}
	format := e.Config.DateFormat
	if format == "" {
		format = "yyyy-MM-dd"
	}
	var out strings.Builder
	out.WriteString(csvLine(header))
	for i := 0; i < 1000; i++ {
		var row []string
		for _, col := range columns {
			if v, ok := randomValue(e, col); ok {
				row = append(row, v)
				continue
			}
			switch col.Type {
			case "intgr":
				row = append(row, strconv.Itoa(rand.Intn(100000)))
			case "date":
				row = append(row, cldr.Format(randomDate(), format))
			case "daterange":
				d1, d2 := randomDate(), randomDate()
				if d1.After(d2) {
					d1, d2 = d2, d1
				}
				row = append(row, cldr.Format(d1, format)+" - "+cldr.Format(d2, format))
			default:
				row = append(row, "String text")
			}
		}
		out.WriteString(csvLine(row))
	}
	if err := os.WriteFile(env.Path("new.csv"), []byte(out.String()), 0o666); err != nil {
		Die("new.csv: %s", perlErr(err))
	}
}
