package web

import "testing"

func TestDefaultViewThresholds(t *testing.T) {
	cases := []struct {
		n      int64
		full   bool
		expect string
	}{
		{3, false, "tree"},
		{20, false, "tree"},
		{21, false, "compact"},
		{200, false, "compact"},
		{201, false, "fan"},
		{500, false, "fan"},
		{3, true, "tree"},
		{50, true, "compact"},
		{201, true, "graph"},
		{5000, true, "graph"},
	}
	for _, c := range cases {
		if got := defaultView(c.n, c.full); got != c.expect {
			t.Errorf("defaultView(%d, %v) = %q, want %q", c.n, c.full, got, c.expect)
		}
	}
}

func TestResolveView(t *testing.T) {
	if resolveView("", "tree") != "tree" {
		t.Error("empty query should fall back")
	}
	if resolveView("bogus", "compact") != "compact" {
		t.Error("invalid query should fall back")
	}
	for _, v := range []string{"tree", "compact", "fan", "graph", "list"} {
		if resolveView(v, "tree") != v {
			t.Errorf("resolveView(%q) should pass through", v)
		}
	}
}
