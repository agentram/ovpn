package subscription

import (
	"sync"
	"time"
)

const (
	activationLimit       = 8
	activationWindow      = 5 * time.Minute
	maxActivationSubjects = 16384
)

type activationCounter struct {
	count int
	reset time.Time
}

type activationLimiter struct {
	mu       sync.Mutex
	subjects map[string]activationCounter
	sweep    time.Time
}

func (l *activationLimiter) allow(subject string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.sweep) {
		for id, counter := range l.subjects {
			if !now.Before(counter.reset) {
				delete(l.subjects, id)
			}
		}
		l.sweep = now.Add(activationWindow)
	}
	if l.subjects == nil {
		l.subjects = make(map[string]activationCounter)
	}
	counter, exists := l.subjects[subject]
	if !exists && len(l.subjects) >= maxActivationSubjects {
		return false
	}
	if !now.Before(counter.reset) {
		counter = activationCounter{reset: now.Add(activationWindow)}
	}
	if counter.count >= activationLimit {
		return false
	}
	counter.count++
	l.subjects[subject] = counter
	return true
}
