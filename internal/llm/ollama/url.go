package ollama

import "strings"

func rootURL(baseURL string) string {
	root := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	return root
}
