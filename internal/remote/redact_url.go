package remote

import (
	"encoding/json"
	"net/url"
)

// redactURLForLog убирает token= из query для логов GET.
func redactURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Get("token") != "" {
		q.Set("token", "[redacted]")
		u.RawQuery = q.Encode()
	}
	s := u.String()
	if len(s) > 512 {
		return s[:512] + "..."
	}
	return s
}

// redactRequestDump сериализует Request в JSON и прогоняет через redactJSONString.
func redactRequestDump(req *Request) string {
	if req == nil {
		return "null"
	}
	b, err := json.Marshal(req)
	if err != nil {
		return "{}"
	}
	return redactJSONString(string(b))
}
