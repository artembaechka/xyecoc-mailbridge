package identity

import "strings"

// ExpandLocalPart appends defaultDomain when login has no "@".
// defaultDomain may be "xyecoc.com" or "@xyecoc.com".
func ExpandLocalPart(login, defaultDomain string) string {
	login = strings.TrimSpace(login)
	defaultDomain = strings.TrimSpace(defaultDomain)
	defaultDomain = strings.TrimPrefix(defaultDomain, "@")
	if defaultDomain == "" || strings.Contains(login, "@") {
		return login
	}
	return login + "@" + defaultDomain
}

// RemoteAuthEmail transforms the IMAP/SMTP login for POST account/authorization.
// If localPartOnly is true, only the part before "@" is sent (API ожидает "baechka", не "baechka@xyecoc.com").
func RemoteAuthEmail(login string, localPartOnly bool) string {
	login = strings.TrimSpace(login)
	if !localPartOnly || login == "" {
		return login
	}
	// Берём локальную часть до первого @ (как в обычном email).
	if i := strings.Index(login, "@"); i > 0 {
		return login[:i]
	}
	return login
}
