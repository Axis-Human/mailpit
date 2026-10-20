package server

import (
	"sync"
	"time"
)

const (
	// loginWindow is how long failed login attempts are remembered
	loginWindow = 15 * time.Minute

	// maxEmailFailures is the number of failed logins allowed for an email address within the window
	maxEmailFailures = 10

	// maxIPFailures is the number of failed logins allowed for a client IP within the window
	maxIPFailures = 50
)

// loginLimiter slows down password guessing by temporarily blocking logins after repeated failures,
// both for an email address and for a client address.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

var logins = &loginLimiter{failures: map[string][]time.Time{}}

// recent returns the failures within the window, and stores the pruned list. mu must be held.
func (l *loginLimiter) recent(key string, now time.Time) []time.Time {
	list := l.failures[key]
	keep := list[:0]

	for _, t := range list {
		if now.Sub(t) < loginWindow {
			keep = append(keep, t)
		}
	}

	if len(keep) == 0 {
		delete(l.failures, key)
		return nil
	}

	l.failures[key] = keep

	return keep
}

// blocked returns whether too many logins have failed for the email address or the IP
func (l *loginLimiter) blocked(email, ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	return len(l.recent("email:"+email, now)) >= maxEmailFailures || len(l.recent("ip:"+ip, now)) >= maxIPFailures
}

// fail records a failed login
func (l *loginLimiter) fail(email, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// do not let the list grow without limit
	if len(l.failures) > 10000 {
		l.failures = map[string][]time.Time{}
	}

	l.failures["email:"+email] = append(l.recent("email:"+email, now), now)
	l.failures["ip:"+ip] = append(l.recent("ip:"+ip, now), now)
}

// success clears the failures of the email address
func (l *loginLimiter) success(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.failures, "email:"+email)
}
