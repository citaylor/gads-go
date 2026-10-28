package lua

import "testing"

func TestSmoke(t *testing.T) {
	it, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	r, err := it.Run("x = vars[\"x\"]\n return x.year / 10", map[string]any{"x": map[string]any{"year": Int(2014)}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", r)
	r, err = it.Run("return {100, 200}", nil, nil, nil)
	t.Logf("%#v %v", r, err)
	r, err = it.Run("return nil + 1", nil, nil, nil)
	t.Logf("%#v %v", r, err)
	r, err = it.Run("return (", nil, nil, nil)
	t.Logf("%#v %v", r, err)
	r, err = it.Run("return f(1)", nil, nil, nil)
	t.Logf("%#v %v", r, err)
}
