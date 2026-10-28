package conformance

import (
	"fmt"
	"strings"

	"github.com/ctrlo/gads-go/internal/gads"
)

// Fixture is the database of a scenario kept open after its steps have
// run, for the database units of the benchmarks (internal/benchunits),
// which time engine calls on it. bench_units.pl sets up the same scenario
// with the Perl runner.
type Fixture struct {
	r *Runner
}

// Setup runs the steps of a scenario file and returns its database, or an
// error if a step fails.
func Setup(file string) (*Fixture, error) {
	scenario, err := loadScenario(file)
	if err != nil {
		return nil, err
	}
	r, err := newRunner(file, scenario)
	if err != nil {
		return nil, err
	}
	var failed []string
	gerr := gads.Try(func() {
		for _, sr := range r.runSteps() {
			if !sr.Pass {
				failed = append(failed, sr.ID+": "+strings.Join(sr.Failures, "; "))
			}
		}
	})
	if gerr == nil && len(failed) > 0 {
		gerr = fmt.Errorf("%s: steps failed: %s", file, strings.Join(failed, ", "))
	}
	if gerr != nil {
		r.close()
		return nil, gerr
	}
	return &Fixture{r: r}, nil
}

// Engine is the engine on the fixture's database.
func (f *Fixture) Engine() *gads.Engine { return f.r.e }

// Layout is the layout of the sheet of the alias (the "as" of its step).
func (f *Fixture) Layout(sheet string) *gads.Layout { return f.r.sheet(sheet).Layout() }

// User is the user of the sheet of the alias.
func (f *Fixture) User(sheet string) *gads.User { return f.r.sheet(sheet).User() }

// Column is the column of the layout by name (as in the scenario steps).
func (f *Fixture) Column(layout *gads.Layout, name string) *gads.Column { return f.r.col(layout, name) }

// View is the view of the alias (the "as" of its view step).
func (f *Fixture) View(alias string, layout *gads.Layout) *gads.View {
	id, ok := f.r.views[alias]
	if !ok {
		gads.Panic("Unknown view %s", alias)
	}
	return f.r.e.LoadView(id, layout)
}

// Close closes the fixture's database.
func (f *Fixture) Close() { f.r.close() }
