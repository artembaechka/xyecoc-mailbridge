package remote

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// GetAttachmentBytes loads attachment bytes: inline base64 from API, optional url field, then GET URL candidates.
func (c *Client) GetAttachmentBytes(ctx context.Context, a Attachment, mailID int, mailCreated string) ([]byte, error) {
	if strings.TrimSpace(a.Content) != "" {
		if b, err := DecodeAttachmentContent(a); err == nil && len(b) > 0 {
			return b, nil
		}
	}
	if u := strings.TrimSpace(a.URL); u != "" {
		for _, origin := range attachmentHTTPOrigins(c) {
			full := resolveMaybeRelativeURL(u, origin)
			if full == "" {
				continue
			}
			for _, bearer := range []bool{false, true} {
				if b, err := c.downloadGET(ctx, full, bearer); err == nil && len(b) > 0 {
					return b, nil
				}
			}
		}
	}

	tok := c.Token()
	bases := attachmentHTTPOrigins(c)

	var lastErr error
	for _, base := range bases {
		for _, u := range AttachmentURLCandidates(base, a, tok, mailCreated, mailID) {
			for _, bearer := range []bool{false, true} {
				b, err := c.downloadGET(ctx, u, bearer)
				if err == nil && len(b) > 0 {
					return b, nil
				}
				lastErr = err
			}
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("empty attachment body for id=%d", a.ID)
}

// attachmentHTTPOrigins lists GET origins for /data/attachments/mails/...
// CDN (REMOTE_ASSET_BASE) first — files are served there; API is fallback.
func attachmentHTTPOrigins(c *Client) []string {
	asset := strings.TrimRight(c.MailAssetOrigin(), "/")
	api := strings.TrimRight(c.AttachmentOrigin(), "/")
	var out []string
	seen := make(map[string]struct{})
	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(asset)
	add(api)
	return out
}

func resolveMaybeRelativeURL(ref, apiBase string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	apiBase = strings.TrimRight(apiBase, "/")
	if strings.HasPrefix(ref, "/") {
		return apiBase + ref
	}
	return apiBase + "/" + strings.TrimPrefix(ref, "/")
}

// AttachmentURLCandidates lists GET URLs to try for /data/attachments/... style APIs.
func AttachmentURLCandidates(base string, a Attachment, token, mailCreated string, mailID int) []string {
	base = strings.TrimRight(base, "/")
	ext := attachmentPathExt(a)
	tok := url.QueryEscape(token)

	primaryDay := attachmentPathDay(a.CreatedAt, mailCreated)
	var mailDay string
	if strings.TrimSpace(mailCreated) != "" {
		mailDay = attachmentPathDay(mailCreated, "")
	}

	var out []string
	seen := make(map[string]struct{})
	add := func(path string) {
		u := fmt.Sprintf("%s%s?token=%s", base, path, tok)
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	add(fmt.Sprintf("/data/attachments/mails/%s/%d.%s", primaryDay, a.ID, ext))
	if mailDay != "" && mailDay != primaryDay {
		add(fmt.Sprintf("/data/attachments/mails/%s/%d.%s", mailDay, a.ID, ext))
	}
	add(fmt.Sprintf("/data/attachments/mails/%d.%s", a.ID, ext))
	if mailID > 0 {
		add(fmt.Sprintf("/data/attachments/mails/%d/%d.%s", mailID, a.ID, ext))
	}
	return out
}
