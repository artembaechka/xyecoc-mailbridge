package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientAuthorize_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/request" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": float64(1),
			"data": map[string]string{
				"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.sig",
			},
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "ru", 2*time.Second)
	if err := c.Authorize(context.Background(), "u@example.com", "secret"); err != nil {
		t.Fatal(err)
	}
	if c.Token() == "" {
		t.Fatal("expected token set")
	}
}

func TestClientAuthorize_Fail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": float64(0),
		})
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "ru", 2*time.Second)
	if err := c.Authorize(context.Background(), "u@example.com", "bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMergeMailListFields_Nested(t *testing.T) {
	raw := []byte(`{"status":1,"data":{"mails":[{"id":1,"from_name":"a","snippet":"","read":true,"important":false,"created_at":"2026-01-01T00:00:00Z","tag_name":""}],"total":{"count":1}}}`)
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	_ = MergeMailListFields(&resp)
	if len(resp.Mails) != 1 || resp.Mails[0].ID != 1 {
		t.Fatalf("mails: %+v", resp.Mails)
	}
}
