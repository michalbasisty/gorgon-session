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

func TestCornerPos(t *testing.T) {
	cases := []struct {
		name       string
		pos        string
		wa         rect
		wantX, wantY int32
	}{
		{"top-left", "top-left", rect{0, 0, 1920, 1040}, 12, 12},
		{"top-right", "top-right", rect{0, 0, 1920, 1040}, 1448, 12},
		{"bottom-left", "bottom-left", rect{0, 0, 1920, 1040}, 12, 268},
		{"bottom-right", "bottom-right", rect{0, 0, 1920, 1040}, 1448, 268},
		{"unknown falls back to bottom-right", "garbage", rect{0, 0, 1920, 1040}, 1448, 268},
		{"empty falls back to bottom-right", "", rect{0, 0, 1920, 1040}, 1448, 268},
		{"non-zero origin top-left", "top-left", rect{100, 50, 1500, 800}, 112, 62},
		{"non-zero origin bottom-right", "bottom-right", rect{100, 50, 1500, 800}, 1028, 28},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y := cornerPos(c.pos, c.wa)
			if x != c.wantX || y != c.wantY {
				t.Errorf("cornerPos(%q, %+v) = (%d, %d), want (%d, %d)",
					c.pos, c.wa, x, y, c.wantX, c.wantY)
			}
		})
	}
}
