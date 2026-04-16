package remote

import (
	"encoding/json"
	"strconv"
	"strings"
)

const maxLogJSON = 24000 // символов ответа в лог (обрезка)

// redactJSONString убирает секреты из произвольного JSON для логов.
func redactJSONString(s string) string {
	if s == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return truncate(s, maxLogJSON)
	}
	redactValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return truncate(s, maxLogJSON)
	}
	ss := string(out)
	if len(ss) > maxLogJSON {
		return ss[:maxLogJSON] + `"...[truncated]"`
	}
	return ss
}

func redactValue(v any) {
	switch x := v.(type) {
	case map[string]any:
		redactMap(x)
	case []any:
		for i := range x {
			redactValue(x[i])
		}
	}
}

func redactMap(m map[string]any) {
	for k, val := range m {
		lk := strings.ToLower(k)
		switch {
		case lk == "password" || lk == "pass" || lk == "secret":
			m[k] = "[redacted]"
		case lk == "token" || lk == "authorization" || strings.HasSuffix(lk, "_token"):
			m[k] = redactTokenString(stringify(val))
		case lk == "mails":
			sl := castSlice(val)
			if len(sl) > 8 {
				m[k] = append([]any{}, sl[:8]...)
				m["_mails_omitted"] = len(sl) - 8
			}
		case lk == "data" && isMap(val):
			redactMap(val.(map[string]any))
		default:
			if mm, ok := val.(map[string]any); ok {
				redactMap(mm)
			} else {
				redactValue(val)
			}
		}
	}
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func castSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func isMap(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// redactTokenString оставляет подсказку длины, без утечки JWT.
func redactTokenString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 12 {
		return "[token len=" + strconv.Itoa(len(s)) + "]"
	}
	return s[:4] + "…[redacted " + strconv.Itoa(len(s)) + " chars]…" + s[len(s)-4:]
}
