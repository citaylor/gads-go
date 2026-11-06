package textcsv

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The rows and the error of each input are those of Perl's Text::CSV
// (conformance/perl/gen_csv_golden.pl)
func TestGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			In    string     `json:"in"`
			Rows  [][]string `json:"rows"`
			Error int        `json:"error"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, c := range golden.Cases {
		r := NewReader(strings.NewReader(c.In))
		rows := [][]string{}
		code := 0
		for len(rows) <= 100 {
			row, err := r.Read()
			if err == io.EOF {
				break
			}
			var e *Error
			if errors.As(err, &e) {
				code = e.Code
				break
			}
			rows = append(rows, row)
		}
		if c.Rows == nil {
			c.Rows = [][]string{}
		}
		if !reflect.DeepEqual(rows, c.Rows) || code != c.Error {
			if failed++; failed <= 15 {
				t.Errorf("%q: got %q %d, Perl %q %d", c.In, rows, code, c.Rows, c.Error)
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d differ", failed, len(golden.Cases))
	}
}
