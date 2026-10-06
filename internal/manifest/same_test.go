package manifest

import "testing"

func TestSameVar(t *testing.T) {
	v := func(s string) *string { return &s }
	cases := []struct {
		a, b Var
		want bool
	}{
		{Var{Value: v("x")}, Var{Value: v("x")}, true},
		{Var{Value: v("x")}, Var{Value: v("y")}, false},
		{Var{Secret: true}, Var{Secret: true}, true},
		{Var{Secret: true}, Var{Value: v("x")}, false},
		{Var{}, Var{Secret: true}, false},
	}
	for _, c := range cases {
		if got := SameVar(c.a, c.b); got != c.want {
			t.Errorf("SameVar(%+v, %+v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
