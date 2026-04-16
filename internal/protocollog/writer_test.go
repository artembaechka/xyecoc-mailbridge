package protocollog

import "testing"

func TestRedactIMAPLine_LOGIN(t *testing.T) {
	got := redactIMAPLine(`A001 LOGIN user@x.com secret123`)
	want := `A001 LOGIN user@x.com [redacted]`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRedactSMTP_AUTH(t *testing.T) {
	got := RedactSMTP(`AUTH PLAIN dGVzdA==`)
	if got == `AUTH PLAIN dGVzdA==` {
		t.Fatal("expected redaction")
	}
}
