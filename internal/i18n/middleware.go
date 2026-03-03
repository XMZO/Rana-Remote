package i18n

import (
	"context"
	"net/http"
)

type localeContextKey struct{}

const LocaleCookieName = "rana_locale"

func LocaleFromContext(ctx context.Context) string {
	v := ctx.Value(localeContextKey{})
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Middleware resolves locale based on configured source order.
func Middleware(tr *Translator, sourceOrder []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			candidates := make([]string, 0, len(sourceOrder)+1)
			for _, src := range sourceOrder {
				switch src {
				case "query":
					candidates = append(candidates, r.URL.Query().Get("lang"))
				case "cookie":
					if c, err := r.Cookie(LocaleCookieName); err == nil {
						candidates = append(candidates, c.Value)
					}
				case "header":
					candidates = append(candidates, r.Header.Get("Accept-Language"))
				}
			}
			locale := tr.ResolveLocale(candidates...)
			ctx := context.WithValue(r.Context(), localeContextKey{}, locale)
			w.Header().Set("Content-Language", locale)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
