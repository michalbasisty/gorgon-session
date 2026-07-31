//go:build windows

package overlay

import "testing"

func TestAlphaFromPercent(t *testing.T) {
	cases := []struct {
		pct  int
		want uintptr
	}{
		{100, 255},
		{98, 249}, // 255*98/100 = 249 (the default normal opacity)
		{78, 198}, // default click-through opacity
		{50, 127},
		{30, 76},
		{0, 76},   // below range clamps to 30%
		{200, 255}, // above range clamps to 100%
	}
	for _, c := range cases {
		if got := alphaFromPercent(c.pct); got != c.want {
			t.Errorf("alphaFromPercent(%d) = %d, want %d", c.pct, got, c.want)
		}
	}
}
