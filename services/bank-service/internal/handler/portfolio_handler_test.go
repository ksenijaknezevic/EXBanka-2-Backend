package handler

import "testing"

// clampPublicQuantity enforces the invariant that the number of shares shown as
// "publicly published for OTC" can never exceed the shares actually held. This is a
// display-side safety net: even if a public_shares row is stale (e.g. left over from
// before a sale), the portfolio never reports more public than owned, for every role
// (client / agent / supervisor) since all share the same aggregation.
func TestClampPublicQuantity(t *testing.T) {
	cases := []struct {
		name string
		pub  int
		net  int64
		want int
	}{
		{"below holdings is unchanged", 3, 10, 3},
		{"equal to holdings is unchanged", 9, 9, 9},
		{"above holdings is clamped to holdings", 9, 2, 2}, // owned 10, published 9, sold 8 → show 2 not 9
		{"zero holdings clamps to zero", 9, 0, 0},
		{"zero public stays zero", 0, 5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampPublicQuantity(tc.pub, tc.net); got != tc.want {
				t.Errorf("clampPublicQuantity(%d, %d) = %d, want %d", tc.pub, tc.net, got, tc.want)
			}
		})
	}
}
