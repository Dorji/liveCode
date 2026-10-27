// Package middleware provides HTTP middleware. Auth depends only on a small
// TokenParser interface so it stays decoupled from the concrete JWT service.
package middleware

import (
	"context"
	"net/http"
	"strings"
)

type TokenParser interface {
	ParseToken(token string) (int64, error)
}

type contextKey string

const userIDKey contextKey = "user_id"

// UserID returns the authenticated user id stored in the context.
func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}

func Auth(parser TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeUnauthorized(w)
				return
			}
			id, err := parser.ParseToken(strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				writeUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
