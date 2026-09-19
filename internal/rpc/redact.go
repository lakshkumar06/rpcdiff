package rpc

import (
	"net/url"
	"regexp"
	"strings"
)

var urlPattern = regexp.MustCompile(`https?://[^\s"']+`)

// RedactURL preserves enough endpoint shape for reports while removing
// credentials from userinfo, common key-bearing path segments, sensitive
// query values, and fragments.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<redacted-url>"
	}
	if u.User != nil {
		u.User = url.User("redacted")
	}
	if u.RawQuery != "" {
		query := u.Query()
		for key := range query {
			if sensitiveKey(key) {
				query.Set(key, "<redacted>")
			}
		}
		u.RawQuery = query.Encode()
	}
	u.Path = redactPath(u.Path)
	u.RawPath = ""
	if u.Fragment != "" {
		u.Fragment = "<redacted>"
	}
	return u.String()
}

// RedactText removes URLs embedded in transport errors before they reach
// terminal output or reports.
func RedactText(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		trailing := ""
		trimmed := strings.TrimRightFunc(match, func(r rune) bool {
			if strings.ContainsRune(".,;:)]}", r) {
				trailing += string(r)
				return true
			}
			return false
		})
		return RedactURL(trimmed) + trailing
	})
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range []string{"key", "token", "secret", "password", "passwd", "auth", "credential"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func redactPath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		if i == 0 || parts[i] == "" {
			continue
		}
		previous := strings.ToLower(parts[i-1])
		if sensitiveKey(parts[i]) || sensitiveKey(previous) || previous == "v1" || previous == "v2" || previous == "v3" || previous == "v4" {
			parts[i] = "<redacted>"
		}
	}
	return strings.Join(parts, "/")
}
