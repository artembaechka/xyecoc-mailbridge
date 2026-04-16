package loginlimit

import (
	"net"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	tcp := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 587}
	if g := ClientIP(tcp); g != "203.0.113.7" {
		t.Fatalf("tcp: got %q", g)
	}
}

func TestTrackerBlock(t *testing.T) {
	tr := New()
	ip := "198.51.100.2"
	for i := 0; i < MaxFailedAttempts-1; i++ {
		tr.RecordFailure(ip)
		if !tr.Allowed(ip) {
			t.Fatalf("should allow before threshold")
		}
	}
	tr.RecordFailure(ip)
	if tr.Allowed(ip) {
		t.Fatal("expected blocked after threshold")
	}
	tr.RecordSuccess(ip)
	if !tr.Allowed(ip) {
		t.Fatal("expected allowed after success")
	}
}

func TestTrackerBlockExpiry(t *testing.T) {
	tr := &Tracker{
		failed:       make(map[string]int),
		blockedUntil: map[string]time.Time{"1.2.3.4": time.Now().Add(-time.Second)},
	}
	if !tr.Allowed("1.2.3.4") {
		t.Fatal("expired block should clear")
	}
}
