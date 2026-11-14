package main

import (
	"fmt"
	"strings"
	"unicode"
)

// initialisms are written in capitals in Go names (Go's convention for acronyms): issuerId is
// IssuerID, sriMessages is SRIMessages.
var initialisms = map[string]bool{
	"api": true, "ca": true, "http": true, "https": true, "id": true, "ip": true, "json": true,
	"pdf": true, "ride": true, "rimpe": true, "ruc": true, "sha256": true, "sri": true,
	"uri": true, "url": true, "uuid": true, "vat": true, "xml": true,
}

// words splits a camelCase, PascalCase or snake_case name into lower-case words: "issuerId" is
// ["issuer", "id"], "fingerprintSha256" is ["fingerprint", "sha256"], "pending_sri" is
// ["pending", "sri"].
func words(name string) []string {
	var result []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			result = append(result, strings.ToLower(string(current)))
			current = current[:0]
		}
	}
	runes := []rune(name)
	for index, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r):
			// A capital starts a word, unless it continues a run of capitals (an acronym) that
			// is not followed by a lower-case letter.
			previousUpper := index > 0 && unicode.IsUpper(runes[index-1])
			nextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if !previousUpper || nextLower {
				flush()
			}
			current = append(current, r)
		default:
			current = append(current, r)
		}
	}
	flush()
	return result
}

// goName is the exported Go name of an API name, with Go's initialisms.
func goName(name string) (string, error) {
	parts := words(name)
	if len(parts) == 0 {
		return "", fmt.Errorf("%q has no letters or digits to name it after", name)
	}
	var builder strings.Builder
	for _, part := range parts {
		if initialisms[part] {
			builder.WriteString(strings.ToUpper(part))
			continue
		}
		runes := []rune(part)
		builder.WriteString(strings.ToUpper(string(runes[0])) + string(runes[1:]))
	}
	result := builder.String()
	if !unicode.IsLetter([]rune(result)[0]) {
		return "", fmt.Errorf("%q does not start with a letter", name)
	}
	return result, nil
}

// constantSuffix names an enum value inside its type's constants: "pending_sri" is PendingSRI,
// "15" is 15 and "*" (every event) is All.
func constantSuffix(value string) (string, error) {
	if value == "*" {
		return "All", nil
	}
	parts := words(value)
	if len(parts) == 0 {
		return "", fmt.Errorf("the enum value %q has no letters or digits to name it after", value)
	}
	var builder strings.Builder
	for _, part := range parts {
		if initialisms[part] {
			builder.WriteString(strings.ToUpper(part))
			continue
		}
		runes := []rune(part)
		builder.WriteString(strings.ToUpper(string(runes[0])) + string(runes[1:]))
	}
	return builder.String(), nil
}
