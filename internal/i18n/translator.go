package i18n

import (
	"fmt"
	"strings"

	"github.com/rana-remote/rana-remote/internal/config"
)

// Translator resolves locale and translates message keys.
type Translator struct {
	bundle    *Bundle
	cfg       config.I18NConfig
	supported map[string]struct{}
}

func NewTranslator(bundle *Bundle, cfg config.I18NConfig) *Translator {
	supported := make(map[string]struct{}, len(cfg.SupportedLocales))
	for _, loc := range cfg.SupportedLocales {
		supported[loc] = struct{}{}
	}
	return &Translator{bundle: bundle, cfg: cfg, supported: supported}
}

func (t *Translator) DefaultLocale() string {
	return t.cfg.DefaultLocale
}

func (t *Translator) SupportedLocales() []string {
	out := make([]string, len(t.cfg.SupportedLocales))
	copy(out, t.cfg.SupportedLocales)
	return out
}

func (t *Translator) IsSupported(locale string) bool {
	_, ok := t.supported[locale]
	return ok
}

func (t *Translator) ResolveLocale(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if t.IsSupported(c) {
			return c
		}
		if idx := strings.IndexByte(c, ','); idx > 0 {
			c = strings.TrimSpace(c[:idx])
		}
		if idx := strings.IndexByte(c, ';'); idx > 0 {
			c = strings.TrimSpace(c[:idx])
		}
		if t.IsSupported(c) {
			return c
		}
		if len(c) >= 2 {
			for loc := range t.supported {
				if strings.HasPrefix(strings.ToLower(loc), strings.ToLower(c[:2])) {
					return loc
				}
			}
		}
	}
	if t.IsSupported(t.cfg.DefaultLocale) {
		return t.cfg.DefaultLocale
	}
	return t.cfg.FallbackLocale
}

func (t *Translator) T(locale, key string, params map[string]any) string {
	if locale == "" || !t.IsSupported(locale) {
		locale = t.cfg.FallbackLocale
	}
	msg, ok := t.bundle.Lookup(locale, key)
	if !ok {
		msg, ok = t.bundle.Lookup(t.cfg.FallbackLocale, key)
		if !ok {
			return key
		}
	}
	for k, v := range params {
		msg = strings.ReplaceAll(msg, "{"+k+"}", fmt.Sprint(v))
	}
	return msg
}
