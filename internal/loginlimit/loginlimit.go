// Package loginlimit tracks failed login attempts per client IP in memory.
package loginlimit

import (
	"net"
	"sync"
	"time"
)

const (
	// MaxFailedAttempts before the IP is temporarily blocked.
	MaxFailedAttempts = 5
	// BlockDuration is how long an IP stays blocked after too many failures.
	BlockDuration = 5 * time.Minute
)

// Tracker is safe for concurrent use.
type Tracker struct {
	mu             sync.Mutex
	failed         map[string]int
	blockedUntil   map[string]time.Time
}

// New returns an empty tracker.
func New() *Tracker {
	return &Tracker{
		failed:       make(map[string]int),
		blockedUntil: make(map[string]time.Time),
	}
}

// ClientIP returns a stable host/IP string for rate limiting (no port).
func ClientIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		if a.IP != nil {
			return a.IP.String()
		}
	case *net.UDPAddr:
		if a.IP != nil {
			return a.IP.String()
		}
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// Allowed reports whether the IP may attempt login (not blocked).
func (t *Tracker) Allowed(ip string) bool {
	if ip == "" || t == nil {
		return true
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if until, ok := t.blockedUntil[ip]; ok {
		if now.Before(until) {
			return false
		}
		delete(t.blockedUntil, ip)
		delete(t.failed, ip)
	}
	return true
}

// RecordFailure increments failed attempts for the IP; at the threshold the IP is blocked.
func (t *Tracker) RecordFailure(ip string) {
	if ip == "" || t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed[ip]++
	if t.failed[ip] >= MaxFailedAttempts {
		t.blockedUntil[ip] = time.Now().Add(BlockDuration)
		delete(t.failed, ip)
	}
}

// RecordSuccess clears failure state for the IP after a successful login.
func (t *Tracker) RecordSuccess(ip string) {
	if ip == "" || t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failed, ip)
	delete(t.blockedUntil, ip)
}
