package remote

import (
	"strings"
	"testing"
)

func TestRedactJSONString_passwordAndToken(t *testing.T) {
	in := `{"service":"account","action":"authorization","data":{"email":"u","password":"secret"},"token":"abc123xyz78901234567890"}`
	out := redactJSONString(in)
	if strings.Contains(out, "secret") {
		t.Fatalf("password leaked: %s", out)
	}
	if !strings.Contains(out, "[redacted]") && !strings.Contains(out, "redacted") {
		t.Fatalf("expected redaction markers: %s", out)
	}
}

func TestRedactURLForLog_tokenQuery(t *testing.T) {
	u := "https://cdn.example.com/data/x?token=REALSECRET&foo=1"
	out := redactURLForLog(u)
	if strings.Contains(out, "REALSECRET") {
		t.Fatalf("token leaked: %s", out)
	}
	if strings.Contains(out, "REALSECRET") || !strings.Contains(out, "redacted") {
		t.Fatalf("expected redacted token in query: %s", out)
	}
}
