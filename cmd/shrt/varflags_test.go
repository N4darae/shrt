package main

import "testing"

func TestVarFlagKeepsTheTypeTheProtoFieldExpects(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"draft=true", true},
		{"draft=false", false},
		{"page_size=7", int64(7)},
		{"rate=1.5", 1.5},
		{"business_date=2026-01-01", "2026-01-01"},
		{"name=true story", "true story"},
		{"code=007", "007"},
		{"amount=1.50", "1.50"},
		{"empty=", ""},
	}
	for _, c := range cases {
		v := varFlags{}
		if err := v.Set(c.in); err != nil {
			t.Fatalf("-var %s: %v", c.in, err)
		}
		for _, got := range v {
			if got != c.want {
				t.Errorf("-var %s gave %#v, want %#v: a bool or number field rejects a quoted string, "+
					"so a chain that runs from its own vars: breaks when the same var is overridden on the command line",
					c.in, got, c.want)
			}
		}
	}
}

func TestVarFlagStillRejectsAValueWithNoKey(t *testing.T) {
	if err := (varFlags{}).Set("no-equals-sign"); err == nil {
		t.Fatal("-var with no = was accepted")
	}
}
