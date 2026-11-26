package telemetry

import (
	"fmt"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var sensitiveKeyParts = []string{
	"password", "passwd", "secret", "token", "sessionid", "cookie",
	"authorization", "credential", "apikey", "privatekey",
}

func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	k = strings.NewReplacer("_", "", "-", "", ".", "").Replace(k)
	for _, p := range sensitiveKeyParts {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

var (
	reURLCreds    = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+):([^\s@/]+)@`)
	reKeyValue    = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|session[_-]?id|csrf[_-]?token)\b(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&)]+)`)
	reBearer      = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
	reSQLPassword = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api_?key)\b(\s*(?:=|:=|\bto\b|\bwith\b)?\s*)'(?:[^']|'')*'`)
	reSessionLike = regexp.MustCompile(`\bsession_id=[A-Za-z0-9_-]{16,}`)
)

func RedactString(s string) string {
	if s == "" {
		return s
	}
	s = reURLCreds.ReplaceAllString(s, "$1:"+Redacted+"@")
	s = reKeyValue.ReplaceAllString(s, "${1}${2}"+Redacted)
	s = reBearer.ReplaceAllString(s, "$1 "+Redacted)
	s = reSessionLike.ReplaceAllString(s, "session_id="+Redacted)
	return s
}

func RedactSQL(q string) string {
	q = reSQLPassword.ReplaceAllString(q, "${1}${2}'"+Redacted+"'")
	return RedactString(q)
}

func RedactParams(query string, params []any) []string {
	if len(params) == 0 {
		return nil
	}
	maskAll := reSQLPassword.MatchString(query) || containsSensitiveWord(query)
	out := make([]string, len(params))
	for i, p := range params {
		if maskAll {
			out[i] = Redacted
			continue
		}
		v := RedactString(fmt.Sprint(p))
		if len(v) > 200 {
			v = v[:200] + "…"
		}
		out[i] = v
	}
	return out
}

func containsSensitiveWord(q string) bool {
	l := strings.ToLower(q)
	for _, w := range []string{"password", "passwd", "secret", "token", "api_key", "apikey"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}
