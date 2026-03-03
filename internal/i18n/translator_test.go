package i18n

import (
	"testing"

	"github.com/rana-remote/rana-remote/internal/config"
)

func TestTranslatorFallback(t *testing.T) {
	bundle := NewBundle()
	tr := NewTranslator(bundle, config.I18NConfig{
		DefaultLocale:     "zh-CN",
		SupportedLocales:  []string{"zh-CN", "en-US"},
		FallbackLocale:    "en-US",
		LocaleSourceOrder: []string{"query", "cookie", "header"},
	})
	msg := tr.T("fr-FR", "auth.unauthorized", nil)
	if msg == "" {
		t.Fatal("message should not be empty")
	}
}
