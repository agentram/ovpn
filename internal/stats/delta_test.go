package stats

import "testing"

func TestComputeDelta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prev      int64
		hasPrev   bool
		current   int64
		wantDelta int64
		wantReset bool
	}{
		{name: "first sample", hasPrev: false, current: 100, wantDelta: 100},
		{name: "increase", prev: 100, hasPrev: true, current: 130, wantDelta: 30},
		{name: "no change", prev: 100, hasPrev: true, current: 100, wantDelta: 0},
		{name: "counter reset", prev: 100, hasPrev: true, current: 5, wantDelta: 5, wantReset: true},
		{name: "counter reset with negative sample clamps to zero", prev: 100, hasPrev: true, current: -5, wantDelta: 0, wantReset: true},
		{name: "negative first sample clamps", hasPrev: false, current: -10, wantDelta: 0},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotDelta, gotReset := computeDelta(tc.prev, tc.hasPrev, tc.current)
			if gotDelta != tc.wantDelta || gotReset != tc.wantReset {
				t.Fatalf("computeDelta(%d,%v,%d)=%d,%v want %d,%v", tc.prev, tc.hasPrev, tc.current, gotDelta, gotReset, tc.wantDelta, tc.wantReset)
			}
		})
	}
}
