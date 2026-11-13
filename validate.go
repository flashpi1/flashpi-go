package flashpi

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// maxTimeout is the longest timeout: the Node SDK's limit (2147483647 ms), so every SDK accepts
// the same values.
const maxTimeout = 2147483647 * time.Millisecond

func validTimeout(timeout time.Duration) error {
	if timeout <= 0 || timeout > maxTimeout {
		return &Error{Message: "The timeout must be more than 0 and at most 2147483.647 seconds"}
	}
	return nil
}

func validRetries(retries int) error {
	if retries < 0 {
		return &Error{Message: "The number of retries must be 0 or more"}
	}
	return nil
}

// isToken reports whether name is a header name (an RFC 9110 token).
func isToken(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// headerValue is a header value without the spaces, tabs and line breaks around it, as the
// Fetch standard's Headers sends it. A line break, NUL or another control character other than
// a tab left inside it is refused, and so is text that is not UTF-8, before anything is sent:
// a bad value is never retried as a network failure. Other text outside ASCII is sent as UTF-8.
// The message never shows the value, which may be a secret.
func headerValue(label, value string) (string, error) {
	trimmed := strings.Trim(value, " \t\r\n")
	for i := 0; i < len(trimmed); i++ {
		if c := trimmed[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return "", &Error{Message: label + " has a line break or another control character"}
		}
	}
	if !utf8.ValidString(trimmed) {
		return "", &Error{Message: label + " has text that is not UTF-8"}
	}
	return trimmed, nil
}

// checkHeaders checks the name and the value of every header (see headerValue) and refuses
// Content-Length and Transfer-Encoding, which the HTTP client sets from the body.
func checkHeaders(label string, headers []header) ([]header, error) {
	checked := make([]header, 0, len(headers))
	for _, h := range headers {
		if !isToken(h.name) {
			return nil, &Error{Message: fmt.Sprintf("%s: %q is not a valid header name", label, h.name)}
		}
		if strings.EqualFold(h.name, "Content-Length") || strings.EqualFold(h.name, "Transfer-Encoding") {
			return nil, &Error{Message: fmt.Sprintf("%s: %s is set by the SDK from the body: leave it out", label, h.name)}
		}
		value, err := headerValue(fmt.Sprintf("The value of header %q", h.name), h.value)
		if err != nil {
			return nil, err
		}
		checked = append(checked, header{h.name, value})
	}
	return checked, nil
}

// jsSpace reports what JavaScript's trim() removes around a string, the Node SDK's key included:
// its white space and line breaks, and the byte order mark (U+FEFF).
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return 0x2000 <= r && r <= 0x200a
}

// apiKeyProblem says why a trimmed key cannot be used, or "". The message never shows the key.
func apiKeyProblem(key string) string {
	switch {
	case key == "":
		return "Missing API key: pass it with flashpi.WithAPIKey or set the FLASHPI_API_KEY environment variable"
	case strings.HasPrefix(key, "whsec_"):
		return "That is a webhook signing secret (whsec_...), not an API key: use it with flashpi.UnwrapWebhook"
	case !strings.HasPrefix(key, "fp_test_") && !strings.HasPrefix(key, "fp_live_"):
		return "The API key must start with fp_test_ or fp_live_"
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return "The API key has spaces, line breaks or other characters an API key cannot have: copy it again from the Flashpi console"
		}
	}
	return ""
}

// pathOf builds a path from a template with {} placeholders, each id percent-encoded as
// JavaScript's encodeURIComponent does. Empty ids, and "." and "..", which URL normalization
// would turn into another resource, are refused.
func pathOf(template string, ids ...string) (string, error) {
	parts := strings.Split(template, "{}")
	if len(parts) != len(ids)+1 {
		panic("flashpi: " + template + " does not have one placeholder per id")
	}
	shown := strings.ReplaceAll(template, "{}", "{id}")
	var builder strings.Builder
	builder.WriteString(parts[0])
	for index, id := range ids {
		if strings.TrimSpace(id) == "" {
			return "", &Error{Message: fmt.Sprintf("Missing id in %s: got %q", shown, id)}
		}
		if id == "." || id == ".." {
			return "", &Error{Message: fmt.Sprintf("Invalid id in %s: %q", shown, id)}
		}
		builder.WriteString(escapeComponent(id))
		builder.WriteString(parts[index+1])
	}
	return builder.String(), nil
}

// escapeComponent percent-encodes everything but A-Z a-z 0-9 and -_.!~*'(), as
// encodeURIComponent.
func escapeComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			builder.WriteByte(c)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(hex[c>>4])
		builder.WriteByte(hex[c&15])
	}
	return builder.String()
}
