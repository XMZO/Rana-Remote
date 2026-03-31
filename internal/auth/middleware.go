package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey string

const userContextKey contextKey = "auth_user"

type ContextUser struct {
	ID     string
	Role   string
	Locale string
}

func UserFromContext(ctx context.Context) (ContextUser, bool) {
	v := ctx.Value(userContextKey)
	u, ok := v.(ContextUser)
	return u, ok
}

func Middleware(tokens *TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""
			authz := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
				token = strings.TrimSpace(authz[len("Bearer "):])
			}
			if token == "" {
				token = strings.TrimSpace(r.URL.Query().Get("access_token"))
			}
			if token == "" {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized", "missing access token")
				return
			}
			claims, err := tokens.Parse(token, "access")
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, ContextUser{ID: claims.UserID, Role: claims.Role, Locale: claims.Locale})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequirePermission(perm Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized", "missing auth context")
				return
			}
			if !HasPermission(user.Role, perm) {
				writeAuthError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": msg,
	})
}
