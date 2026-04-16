package remote

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Request mirrors the web client's POST /request JSON body (see main.js requester / socket.emit).
type Request struct {
	Service     string         `json:"service"`
	Action      string         `json:"action"`
	Token       string         `json:"token,omitempty"`
	CurrentLang string         `json:"currentLang,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
	Data        map[string]any `json:"data,omitempty"`

	Page       int    `json:"page,omitempty"`
	LastMailID int    `json:"last_mail_id,omitempty"`
	SearchText string `json:"search_text,omitempty"`

	DraftID any    `json:"draft_id,omitempty"`
	Draft   *bool  `json:"draft,omitempty"` // pointer: omit only when nil; false must serialize as false
	PageID  any  `json:"page_id,omitempty"`
}

// APIStatus is the numeric API result code (1 = success). The server sometimes
// sends it as a JSON string ("1") or omits it when the payload is still valid.
type APIStatus int

// UnmarshalJSON accepts number, string, bool, or null.
func (s *APIStatus) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*s = 0
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*s = APIStatus(n)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*s = APIStatus(int(f))
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		v, _ := strconv.Atoi(strings.TrimSpace(str))
		*s = APIStatus(v)
		return nil
	}
	var bl bool
	if err := json.Unmarshal(b, &bl); err == nil {
		if bl {
			*s = 1
		} else {
			*s = 0
		}
		return nil
	}
	*s = 0
	return nil
}

// Response is a union of several remote shapes; only relevant fields are set per action.
type Response struct {
	Status  APIStatus `json:"status"`
	Message string    `json:"message,omitempty"`

	Service string `json:"service,omitempty"`
	Action  string `json:"action,omitempty"`

	Mails      []MailSummary `json:"mails,omitempty"`
	Total      *TotalCount   `json:"total,omitempty"`
	LastMailID int           `json:"last_mail_id,omitempty"`
	NothingChanged bool      `json:"nothing_changed,omitempty"`

	Folders []Folder `json:"folders,omitempty"`
	Tags    []Tag    `json:"tags,omitempty"`
	Params  map[string]any `json:"params,omitempty"`

	Data json.RawMessage `json:"data,omitempty"`
}

type TotalCount struct {
	Count int `json:"count"`
}

// UnmarshalJSON accepts count as number or string (API sometimes returns "2").
func (t *TotalCount) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var aux struct {
		Count json.RawMessage `json:"count"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	if len(aux.Count) == 0 {
		return nil
	}
	var f float64
	if err := json.Unmarshal(aux.Count, &f); err == nil {
		t.Count = int(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(aux.Count, &s); err == nil {
		n, _ := strconv.Atoi(strings.TrimSpace(s))
		t.Count = n
		return nil
	}
	return nil
}

type MailSummary struct {
	ID         int    `json:"id"`
	FromName   string `json:"from_name"`
	FromEmail  string `json:"from_email"`
	Sender     string `json:"sender"`
	Snippet    string `json:"snippet"`
	Subject    string `json:"subject"`
	Read       bool   `json:"read"`
	Important  bool   `json:"important"`
	CreatedAt  string `json:"created_at"`
	TagName    string `json:"tag_name"`
}

// EffectiveFrom prefers from_name, falls back to sender (API variants).
func (m *MailSummary) EffectiveFrom() string {
	if strings.TrimSpace(m.FromName) != "" {
		return strings.TrimSpace(m.FromName)
	}
	return strings.TrimSpace(m.Sender)
}

// EffectiveSubject uses subject, then snippet for preview.
func (m *MailSummary) EffectiveSubject() string {
	if strings.TrimSpace(m.Subject) != "" {
		return strings.TrimSpace(m.Subject)
	}
	return strings.TrimSpace(m.Snippet)
}

type MailDetail struct {
	ID           int            `json:"id"`
	Subject      string         `json:"subject"`
	FromName     string         `json:"from_name"`
	FromEmail    string         `json:"from_email"`
	To           string         `json:"to"`
	CreatedAt    string         `json:"created_at"`
	Message      string         `json:"message,omitempty"`
	Attachments  []Attachment   `json:"attachments"`
	Attaches     []Attachment   `json:"attaches,omitempty"` // alternate API key
}

type Attachment struct {
	ID         int    `json:"id"`
	FileName   string `json:"file_name"`
	FileSize   int    `json:"file_size"`
	Extension  string `json:"extension"`
	CreatedAt  string `json:"created_at"`
	Content    string `json:"content,omitempty"` // base64 in draft payload
	URL        string `json:"url,omitempty"`     // optional absolute or site-relative download URL from API
}

type Folder struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	From     string `json:"from,omitempty"`
	Contains string `json:"contains,omitempty"`
}

type Tag struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ResponseErrMsg returns root `message` or `data.message` when present.
func ResponseErrMsg(resp *Response) string {
	s := strings.TrimSpace(resp.Message)
	if s != "" {
		return s
	}
	if len(resp.Data) == 0 {
		return ""
	}
	var aux struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp.Data, &aux); err != nil {
		return ""
	}
	return strings.TrimSpace(aux.Message)
}

// MailListResponseOK is true for a successful mail/default (or equivalent) list
// payload. Some responses omit `status` at the root while still returning mails
// or total.count.
func MailListResponseOK(resp *Response) bool {
	if resp.Status == 1 {
		return true
	}
	if len(resp.Mails) > 0 {
		return true
	}
	if resp.Total != nil && resp.Total.Count == 0 {
		return true
	}
	return false
}

// MailMutationOK is true for successful mail/message-new, reply-confirm, draft save, etc.
// Some APIs nest status under data or send message "success" with status 0 at root.
func MailMutationOK(resp *Response) bool {
	if resp == nil {
		return false
	}
	if resp.Status == 1 {
		return true
	}
	m := strings.ToLower(strings.TrimSpace(resp.Message))
	if m == "success" || m == "ok" {
		return true
	}
	if len(resp.Data) == 0 {
		return false
	}
	var nested struct {
		Status APIStatus `json:"status"`
	}
	if err := json.Unmarshal(resp.Data, &nested); err == nil && nested.Status == 1 {
		return true
	}
	return false
}
