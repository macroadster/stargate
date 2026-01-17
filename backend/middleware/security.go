package middleware

import (
	"net/http"
	"strings"
)

// ValidateFilename is middleware to check request parameters for path traversal
func ValidateFilename(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, values := range r.URL.Query() {
			for _, value := range values {
				if strings.Contains(value, "../") || strings.Contains(value, "..\\") {
					http.Error(w, "invalid input: path traversal detected", http.StatusBadRequest)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
