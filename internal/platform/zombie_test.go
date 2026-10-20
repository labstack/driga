package platform

import "testing"

func TestParseProcStatZombie(t *testing.T) {
	for _, tc := range []struct {
		stat string
		want bool
	}{
		{"9441 (agent (worker)) Z 9000 0 0 0", true},
		{"9441 (Z worker) S 9000 0 0 0", false},
		{"9441 (worker) R 9000 0 0 0", false},
		{"9441 (worker) T 9000 0 0 0", false},
		{"9441 (worker) X 9000 0 0 0", false},
		{"", false},
		{"9441 (worker)", false},
		{"9441 worker Z", false},
		{"9441 (worker) ZZ 9000", false},
	} {
		if got := procStatZombie([]byte(tc.stat)); got != tc.want {
			t.Errorf("%q: zombie = %t, want %t", tc.stat, got, tc.want)
		}
	}
}
