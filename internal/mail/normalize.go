package mail

import (
	"regexp"
	"strings"
)

var quotedHeader = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^on .{4,160} wrote:\s*$`),
	regexp.MustCompile(`(?i)^[-]{2,}\s*(original message|forwarded message)\s*[-]{2,}$`),
	regexp.MustCompile(`(?i)^в\s+.+\s+(написал|написала):\s*$`),
}

// NormalizeBody trims quoted reply history, signatures and whitespace.
// If the new reply is very short, a small piece of its original context is kept.
func NormalizeBody(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	cut := len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "--" || t == "-- " || strings.HasPrefix(t, ">") || isQuoteHeader(t) {
			cut = i
			break
		}
	}
	trimmed := normalizeSpaces(strings.Join(lines[:cut], "\n"))
	if len([]rune(trimmed)) < 80 && cut < len(lines) {
		// Without this, a reply like "OK" loses the question it answered.
		rest := normalizeSpaces(strings.Join(lines[cut:], "\n"))
		if rest != "" {
			rr := []rune(rest)
			if len(rr) > 350 {
				rr = rr[:350]
			}
			trimmed += "\n[Фрагмент предыдущей переписки]\n" + string(rr)
		}
	}
	return strings.TrimSpace(trimmed)
}
func isQuoteHeader(t string) bool {
	for _, r := range quotedHeader {
		if r.MatchString(t) {
			return true
		}
	}
	return false
}
func normalizeSpaces(s string) string {
	var out []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
