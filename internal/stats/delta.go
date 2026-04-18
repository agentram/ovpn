package stats

// computeDelta returns compute delta.
func computeDelta(prevValue int64, hasPrev bool, currentValue int64) (delta int64, reset bool) {
	switch {
	case !hasPrev:
		delta = currentValue
	case currentValue >= prevValue:
		delta = currentValue - prevValue
	default:
		// Counter reset/restart detected.
		delta = currentValue
		reset = true
	}
	if delta < 0 {
		delta = 0
	}
	return delta, reset
}
