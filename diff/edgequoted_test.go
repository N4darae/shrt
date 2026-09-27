package diff

import "testing"

func TestAStringWithEdgeSpacesOrNoTextIsShownQuoted(t *testing.T) {
	for _, c := range []struct {
		change Change
		want   string
	}{
		{Change{Kind: KindChanged, Want: "  name  ", Got: "  name"}, `want="  name  " got="  name"`},
		{Change{Kind: KindChanged, Want: "", Got: "name"}, `want="" got=name`},
		{Change{Kind: KindChanged, Want: 3.0, Got: "a b"}, `want=3 got=a b`},
	} {
		if got := c.change.describe(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
