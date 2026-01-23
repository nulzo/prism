package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	// Regex to tokenize JSON parts:
	// 1. Keys (quoted strings followed by colon)
	// 2. String values (quoted strings)
	// 3. Numbers / Booleans / Null
	jsonTokenRegex = regexp.MustCompile(`("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)`)
)

// HighlightJSON takes a JSON string (minified or indented) and applies ANSI colors.
func HighlightJSON(jsonStr string) string {
	if disableColor {
		return jsonStr
	}

	return jsonTokenRegex.ReplaceAllStringFunc(jsonStr, func(token string) string {
		switch {
		case strings.HasSuffix(token, ":"): // Key ("key":)
			// Strip colon, colorize key, add colon back
			key := token[:len(token)-1]
			return fmt.Sprintf("%s%s%s:", Blue, key, Reset)

		case strings.HasPrefix(token, "\""): // String Value ("value")
			return fmt.Sprintf("%s%s%s", Green, token, Reset)

		case token == "true" || token == "false": // Boolean
			return fmt.Sprintf("%s%s%s", Yellow, token, Reset)

		case token == "null": // Null
			return fmt.Sprintf("%s%s%s", Red, token, Reset)

		default: // Number
			return fmt.Sprintf("%s%s%s", Purple, token, Reset)
		}
	})
}

// PrettyFormat takes any interface, marshals it to indented JSON, and colorizes it.
func PrettyFormat(v interface{}) string {
	// If it's already a []byte or string that looks like JSON, try to format it
	var str string
	switch t := v.(type) {
	case []byte:
		str = string(t)
	case string:
		str = t
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%+v", v)
		}
		str = string(b)
	}

	return HighlightJSON(str)
}
