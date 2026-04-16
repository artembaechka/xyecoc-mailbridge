package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client calls the remote REST API (same contract as the browser's fetch to /request).
type Client struct {
	BaseURL string // POST /request
	// AssetURL: GET /mail/... and (first choice) /data/attachments/... on CDN; empty → same as BaseURL.
	AssetURL string
	HTTP     *http.Client
	Lang     string
	Log      *slog.Logger
	Trace    bool

	mu    sync.Mutex
	token string
}

// MailAssetOrigin is the origin for static mail HTML paths (may differ from API host).
func (c *Client) MailAssetOrigin() string {
	s := strings.TrimSpace(c.AssetURL)
	if s != "" {
		return strings.TrimRight(s, "/")
	}
	return c.BaseURL
}

// AttachmentOrigin is the fallback origin for GET /data/attachments/mails/... (API BaseURL) if CDN GET fails.
func (c *Client) AttachmentOrigin() string {
	return strings.TrimRight(c.BaseURL, "/")
}

func NewClient(baseURL, lang string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP: &http.Client{Timeout: timeout},
		Lang:    lang,
	}
}

func (c *Client) SetToken(tok string) {
	c.mu.Lock()
	c.token = tok
	c.mu.Unlock()
}

func (c *Client) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// postJSON executes POST /request and returns the raw response body (HTTP 200).
func (c *Client) postJSON(ctx context.Context, req *Request) ([]byte, error) {
	if req.CurrentLang == "" {
		req.CurrentLang = c.Lang
	}
	if req.Token == "" {
		req.Token = c.Token()
	}
	if c.Trace && c.Log != nil {
		c.Log.Info("remote HTTP POST request",
			"url", c.BaseURL+"/request",
			"body", redactRequestDump(req),
		)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return c.postJSONBody(ctx, body)
}

func (c *Client) postJSONBody(ctx context.Context, body []byte) ([]byte, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/request", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json;charset=utf-8")
	hreq.Header.Set("Accept", "application/json")

	res, err := c.HTTP.Do(hreq)
	if err != nil {
		if c.Trace && c.Log != nil {
			c.Log.Info("remote HTTP POST error", "err", err.Error())
		}
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		if c.Trace && c.Log != nil {
			c.Log.Info("remote HTTP POST read error", "err", err.Error())
		}
		return nil, err
	}
	if c.Trace && c.Log != nil {
		c.Log.Info("remote HTTP POST response",
			"status_code", res.StatusCode,
			"body", redactJSONString(string(raw)),
		)
	}
	if res.StatusCode != http.StatusOK {
		return raw, fmt.Errorf("remote HTTP %d: %s", res.StatusCode, truncate(string(raw), 512))
	}
	return raw, nil
}

// postJSONMap POSTs a JSON object (e.g. reply-confirm with draft_id: null).
func (c *Client) postJSONMap(ctx context.Context, payload map[string]any) ([]byte, error) {
	if _, ok := payload["token"]; !ok || payload["token"] == nil {
		payload["token"] = c.Token()
	}
	if v, ok := payload["currentLang"]; !ok || v == nil || v == "" {
		payload["currentLang"] = c.Lang
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if c.Trace && c.Log != nil {
		c.Log.Info("remote HTTP POST request",
			"url", c.BaseURL+"/request",
			"body", redactJSONString(string(body)),
		)
	}
	return c.postJSONBody(ctx, body)
}

// DoReplyConfirm sends mail/reply-confirm as the web client does for replies.
// attaches items are maps with "filename" and base64 "content" (see web mail composer).
// currentLang should match the web client (typically "mail"); empty falls back to c.Lang.
func (c *Client) DoReplyConfirm(ctx context.Context, mailID string, messageHTML string, attaches []any, currentLang string) (*Response, error) {
	if attaches == nil {
		attaches = []any{}
	}
	payload := map[string]any{
		"service":  "mail",
		"action":   "reply-confirm",
		"draft_id": nil,
		"data": map[string]any{
			"message":  messageHTML,
			"attaches": attaches,
			"mail_id":  mailID,
		},
	}
	if strings.TrimSpace(currentLang) != "" {
		payload["currentLang"] = currentLang
	}
	raw, err := c.postJSONMap(ctx, payload)
	if err != nil {
		return nil, err
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// Do sends POST /request with JSON body. Token is taken from Client if req.Token is empty.
func (c *Client) Do(ctx context.Context, req *Request) (*Response, error) {
	raw, err := c.postJSON(ctx, req)
	if err != nil {
		return nil, err
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// DoRaw is like Do but returns the raw JSON body (for mail/view where fields may sit outside `data`).
func (c *Client) DoRaw(ctx context.Context, req *Request) ([]byte, error) {
	return c.postJSON(ctx, req)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Authorize performs account/authorization and stores token on success.
func (c *Client) Authorize(ctx context.Context, email, password string) error {
	resp, err := c.Do(ctx, &Request{
		Service: "account",
		Action:  "authorization",
		Data: map[string]any{
			"email":    email,
			"password": password,
		},
	})
	if err != nil {
		return err
	}
	if resp.Status != 1 {
		return fmt.Errorf("login failed: %s", resp.Message)
	}
	tok, err := ExtractToken(resp)
	if err != nil {
		return err
	}
	c.SetToken(tok)
	return nil
}

// RefreshToken calls account/refresh-token.
func (c *Client) RefreshToken(ctx context.Context) error {
	tok := c.Token()
	if tok == "" {
		return fmt.Errorf("no token to refresh")
	}
	resp, err := c.Do(ctx, &Request{
		Service: "account",
		Action:  "refresh-token",
		Data: map[string]any{"token": tok},
		Token:   tok,
	})
	if err != nil {
		return err
	}
	if resp.Status != 1 {
		return fmt.Errorf("refresh failed: %s", resp.Message)
	}
	nt, err := ExtractToken(resp)
	if err != nil {
		return err
	}
	c.SetToken(nt)
	return nil
}

// MailHTMLURL returns CDN URL used by the web client for iframe body (GET).
func MailHTMLURL(httpBase, token string, mailID int) string {
	base := strings.TrimRight(httpBase, "/")
	// main.js: `${api_endpoint_http}/mail/${encodeURIComponent(token)}/${details.id}`
	u := fmt.Sprintf("%s/mail/%s/%d", base, url.PathEscape(token), mailID)
	return u
}

// AttachmentURL returns the first candidate URL from AttachmentURLCandidates (legacy helper).
func AttachmentURL(httpBase string, a Attachment, token string, mailCreatedAt string) string {
	u := AttachmentURLCandidates(strings.TrimRight(httpBase, "/"), a, token, mailCreatedAt, 0)
	if len(u) == 0 {
		return ""
	}
	return u[0]
}

func attachmentPathDay(attCreated, mailCreated string) string {
	for _, s := range []string{attCreated, mailCreated} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		day := s
		if i := strings.IndexByte(day, 'T'); i >= 0 {
			day = day[:i]
		} else if i := strings.IndexByte(day, ' '); i >= 0 {
			day = day[:i]
		}
		day = strings.TrimSpace(day)
		if len(day) >= 8 {
			return day
		}
	}
	return "1970-01-01"
}

func attachmentPathExt(a Attachment) string {
	ext := strings.TrimSpace(strings.ToLower(a.Extension))
	ext = strings.TrimPrefix(ext, ".")
	if ext != "" {
		return ext
	}
	fn := strings.TrimSpace(a.FileName)
	if i := strings.LastIndexByte(fn, '.'); i >= 0 && i < len(fn)-1 {
		return strings.ToLower(strings.TrimSpace(fn[i+1:]))
	}
	return "bin"
}

// DownloadGET performs GET (CDN /mail/..., query ?token=, etc.).
func (c *Client) DownloadGET(ctx context.Context, fullURL string) ([]byte, error) {
	return c.downloadGET(ctx, fullURL, false)
}

// downloadGET optionally adds Authorization: Bearer for attachment endpoints that require it.
func (c *Client) downloadGET(ctx context.Context, fullURL string, bearer bool) ([]byte, error) {
	safeURL := redactURLForLog(fullURL)
	if c.Trace && c.Log != nil {
		c.Log.Info("remote HTTP GET request", "url", safeURL, "bearer", bearer)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if bearer {
		if tok := c.Token(); tok != "" {
			hreq.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	res, err := c.HTTP.Do(hreq)
	if err != nil {
		if c.Trace && c.Log != nil {
			c.Log.Info("remote HTTP GET error", "url", safeURL, "err", err.Error())
		}
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		if c.Trace && c.Log != nil {
			c.Log.Info("remote HTTP GET read error", "url", safeURL, "err", err.Error())
		}
		return nil, err
	}
	if c.Trace && c.Log != nil {
		c.Log.Info("remote HTTP GET response",
			"url", safeURL,
			"status_code", res.StatusCode,
			"content_length", len(b),
			"body", redactJSONString(string(b)),
		)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", safeURL, res.StatusCode)
	}
	return b, nil
}

// DecodeAttachmentContent returns bytes from draft attachment base64 content field.
func DecodeAttachmentContent(a Attachment) ([]byte, error) {
	if a.Content == "" {
		return nil, fmt.Errorf("no content")
	}
	return base64.StdEncoding.DecodeString(a.Content)
}

// CloneWithToken returns a shallow copy with its own token (per IMAP user session).
func (c *Client) CloneWithToken(tok string) *Client {
	n := NewClient(c.BaseURL, c.Lang, c.HTTP.Timeout)
	n.AssetURL = c.AssetURL
	n.HTTP = c.HTTP
	n.Log = c.Log
	n.Trace = c.Trace
	n.SetToken(tok)
	return n
}

func (c *Client) WithTimeout(d time.Duration) *Client {
	n := c.CloneWithToken(c.Token())
	n.HTTP = &http.Client{Timeout: d}
	return n
}
