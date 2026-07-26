package util

import "strings"

func ShortUUID(u string) string {
	if len(u) > 8 {
		return u[:8]
	}
	return u
}

func Truncate(b []byte, max int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
