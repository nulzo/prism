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
	if !Enabled() {
		return jsonStr
	}

	return jsonTokenRegex.ReplaceAllStringFunc(jsonStr, func(token string) string {
		switch {
		case strings.HasSuffix(token, ":"): // Key ("key":)
			// Strip colon, colorize key, add colon back
			key := token[:len(token)-1]
			// Keys are Blue
			return fmt.Sprintf("%s%s%s:", Blue, key, ResetCode)

		case strings.HasPrefix(token, "\""): // String Value ("value")
			// Strings are Green
			return fmt.Sprintf("%s%s%s", Green, token, ResetCode)

		case token == "true" || token == "false": // Boolean
			// Booleans are Yellow
			return fmt.Sprintf("%s%s%s", Yellow, token, ResetCode)

		case token == "null": // Null
			// Null is Red/Grey
			return fmt.Sprintf("%s%s%s", DimCode, token, ResetCode)

		default: // Number
			// Numbers are Purple
			return fmt.Sprintf("%s%s%s", Purple, token, ResetCode)
		}
	})
}

// PrettyFormat takes any interface, marshals it to indented JSON, and colorizes it.
// It returns the string representation.
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

// PrettyPrint prints the PrettyFormatted JSON to stdout with a newline.
func PrettyPrint(v interface{}) {
	fmt.Println(PrettyFormat(v))
}
