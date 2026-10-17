package middleware

import (
	"github.com/google/uuid"
	"strings"
)

// IsFunctionTriggerPath narrowly exempts only the bearer-authenticated function
// trigger endpoint from session authentication. No dashboard route is exempt.
func IsFunctionTriggerPath(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "function-triggers" || len(parts[2]) == 0 || len(parts[2]) > 80 {
		return false
	}
	for _, c := range parts[2] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	id, e := uuid.Parse(parts[3])
	return e == nil && id.String() == parts[3]
}
