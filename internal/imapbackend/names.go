package imapbackend

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// specCacheKey is a stable list-cache key (does not depend on folder display name).
func specCacheKey(s MailboxSpec) string {
	if s.Param2 != "" {
		return s.Param1 + "/" + s.Param2
	}
	return s.Param1
}

func sanitizeIMAPMailboxName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\':
			b.WriteRune('-')
		case '\x00', '\r', '\n':
			continue
		default:
			if unicode.IsControl(r) {
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 200 {
		return out[:200]
	}
	return out
}

// imapMailboxLeaf is the last non-empty segment of a hierarchical mailbox name
// (DELETE/RENAME sometimes pass "parent/Tag-…" instead of a flat LIST name).
func imapMailboxLeaf(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, `\`, "/")
	parts := strings.Split(s, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(parts[i]); t != "" {
			return t
		}
	}
	return s
}

func isReservedStandardName(lower string) bool {
	switch lower {
	case "inbox", "sent", "drafts", "junk", "spam", "trash", "important":
		return true
	default:
		return false
	}
}

// uniqueTagDisplayName is the LIST name for a mail tag: "Tag-" + sanitized label,
// with disambiguation if the name collides (see uniqueDisplayName).
func uniqueTagDisplayName(tagName string, spec MailboxSpec, occupied map[string]struct{}) string {
	base := sanitizeIMAPMailboxName(tagName)
	if base == "" {
		base = spec.Param2
	}
	if isReservedStandardName(strings.ToLower(base)) {
		base = spec.Param2
	}
	candidate := "Tag-" + base
	for i := 0; ; i++ {
		if _, taken := occupied[candidate]; !taken {
			occupied[candidate] = struct{}{}
			return candidate
		}
		if i == 0 {
			candidate = "Tag-" + base + " [" + spec.Param1 + " " + spec.Param2 + "]"
		} else {
			candidate = fmt.Sprintf("Tag-%s~%d", base, i+1)
		}
	}
}

// uniqueDisplayName picks an IMAP-safe, unique mailbox name for LIST.
func uniqueDisplayName(apiName string, spec MailboxSpec, occupied map[string]struct{}) string {
	base := sanitizeIMAPMailboxName(apiName)
	if base == "" {
		base = CanonicalName(spec)
	}
	if isReservedStandardName(strings.ToLower(base)) {
		base = CanonicalName(spec)
	}
	candidate := base
	for i := 0; ; i++ {
		if _, taken := occupied[candidate]; !taken {
			occupied[candidate] = struct{}{}
			return candidate
		}
		if i == 0 {
			candidate = base + " [" + spec.Param1 + " " + spec.Param2 + "]"
		} else {
			candidate = fmt.Sprintf("%s~%d", base, i+1)
		}
	}
}

// MailboxSpec drives the remote "mail" / "default" action params (see main.js pageId).
type MailboxSpec struct {
	Param1 string // inbox, sent, draft, trash, spam, important, folders, tags
	Param2 string // folder or tag id as string
}

func paramsFromSpec(s MailboxSpec) map[string]any {
	m := map[string]any{}
	if s.Param1 != "" {
		m["param_1"] = s.Param1
	}
	if s.Param2 != "" {
		m["param_2"] = s.Param2
	}
	return m
}

// ParseMailboxName maps IMAP mailbox path to remote params.
func ParseMailboxName(name string) (MailboxSpec, error) {
	n := strings.TrimSpace(name)
	low := strings.ToLower(n)

	switch low {
	case "inbox":
		return MailboxSpec{Param1: "inbox"}, nil
	case "sent":
		return MailboxSpec{Param1: "sent"}, nil
	case "drafts":
		return MailboxSpec{Param1: "draft"}, nil
	case "trash":
		return MailboxSpec{Param1: "trash"}, nil
	case "junk", "spam":
		return MailboxSpec{Param1: "spam"}, nil
	case "important":
		return MailboxSpec{Param1: "important"}, nil
	}

	if strings.HasPrefix(low, "folder-") {
		id := strings.TrimPrefix(low, "folder-")
		if _, err := strconv.Atoi(id); err != nil {
			return MailboxSpec{}, fmt.Errorf("bad folder id")
		}
		return MailboxSpec{Param1: "folders", Param2: id}, nil
	}
	if strings.HasPrefix(low, "tag-") {
		id := strings.TrimPrefix(low, "tag-")
		if _, err := strconv.Atoi(id); err != nil {
			return MailboxSpec{}, fmt.Errorf("bad tag id")
		}
		return MailboxSpec{Param1: "tags", Param2: id}, nil
	}

	return MailboxSpec{}, fmt.Errorf("unknown mailbox: %s", name)
}

// CanonicalName returns stable IMAP name for LIST (Folder-12, Tag-3).
func CanonicalName(spec MailboxSpec) string {
	switch spec.Param1 {
	case "inbox":
		return "INBOX"
	case "sent":
		return "Sent"
	case "draft":
		return "Drafts"
	case "trash":
		return "Trash"
	case "spam":
		return "Junk"
	case "important":
		return "Important"
	case "folders":
		return "Folder-" + spec.Param2
	case "tags":
		return "Tag-" + spec.Param2
	default:
		return "INBOX"
	}
}

// tagCreateDisplayName returns the tag label for mail/tag-new when IMAP CREATE uses
// a path whose first segment is "tag" or "Labels" (case-insensitive). Additional
// segments are joined with "/" (then sanitized).
func tagCreateDisplayName(imapName string) (string, bool) {
	s := strings.TrimSpace(imapName)
	if s == "" {
		return "", false
	}
	s = strings.ReplaceAll(s, `\`, "/")
	var segs []string
	for _, p := range strings.Split(s, "/") {
		if t := strings.TrimSpace(p); t != "" {
			segs = append(segs, t)
		}
	}
	if len(segs) < 2 {
		return "", false
	}
	switch strings.ToLower(segs[0]) {
	case "tag", "labels":
		rest := strings.Join(segs[1:], "/")
		rest = sanitizeIMAPMailboxName(rest)
		if rest == "" {
			return "", false
		}
		if isReservedStandardName(strings.ToLower(rest)) {
			return "", false
		}
		return rest, true
	default:
		return "", false
	}
}

// folderNameForAPI maps an IMAP CREATE mailbox argument to mail/folder-new `data.name`
// (last path segment if "/" or "\" appear; then sanitization and reserved-name checks).
func folderNameForAPI(imapName string) (string, error) {
	s := strings.TrimSpace(imapName)
	if s == "" {
		return "", fmt.Errorf("empty mailbox name")
	}
	s = strings.ReplaceAll(s, "\\", "/")
	parts := strings.Split(s, "/")
	var leaf string
	for i := len(parts) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(parts[i]); t != "" {
			leaf = t
			break
		}
	}
	if leaf == "" {
		return "", fmt.Errorf("invalid mailbox name")
	}
	leaf = sanitizeIMAPMailboxName(leaf)
	if leaf == "" {
		return "", fmt.Errorf("invalid mailbox name")
	}
	if isReservedStandardName(strings.ToLower(leaf)) {
		return "", fmt.Errorf("reserved mailbox name")
	}
	if spec, err := ParseMailboxName(leaf); err == nil {
		if spec.Param1 != "folders" && spec.Param1 != "tags" {
			return "", fmt.Errorf("reserved mailbox name")
		}
	}
	return leaf, nil
}

// PageIDForMailboxSpec is the web client's `page_id` for mail-action (e.g. "/inbox/").
func PageIDForMailboxSpec(spec MailboxSpec) string {
	switch spec.Param1 {
	case "inbox":
		return "/inbox/"
	case "sent":
		return "/sent/"
	case "draft":
		return "/draft/"
	case "trash":
		return "/trash/"
	case "spam":
		return "/spam/"
	case "important":
		return "/important/"
	case "folders":
		if spec.Param2 != "" {
			// Как в веб-клиенте: href="/inbox/folders/${id}"
			return "/inbox/folders/" + spec.Param2 + "/"
		}
		return "/inbox/"
	case "tags":
		if spec.Param2 != "" {
			// Как в веб-клиенте: href="/inbox/tags/${id}"
			return "/inbox/tags/" + spec.Param2 + "/"
		}
		return "/inbox/"
	default:
		return "/inbox/"
	}
}
