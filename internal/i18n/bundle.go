package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bundle stores locale dictionaries.
type Bundle struct {
	locales map[string]map[string]string
}

func NewBundle() *Bundle {
	return &Bundle{locales: map[string]map[string]string{
		"zh-CN": defaultZH(),
		"en-US": defaultEN(),
	}}
}

func (b *Bundle) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var data map[string]string
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("parse locale %s: %w", path, err)
		}
		locale := strings.TrimSuffix(e.Name(), ".json")
		if _, ok := b.locales[locale]; !ok {
			b.locales[locale] = make(map[string]string)
		}
		for k, v := range data {
			b.locales[locale][k] = v
		}
	}
	return nil
}

func (b *Bundle) LocaleList() []string {
	out := make([]string, 0, len(b.locales))
	for loc := range b.locales {
		out = append(out, loc)
	}
	return out
}

func (b *Bundle) Lookup(locale, key string) (string, bool) {
	m, ok := b.locales[locale]
	if !ok {
		return "", false
	}
	v, ok := m[key]
	return v, ok
}

func defaultZH() map[string]string {
	return map[string]string{
		"ok":                       "成功",
		"auth.invalid_credentials": "用户名或密码错误",
		"auth.unauthorized":        "未登录或会话已过期",
		"auth.rate_limited":        "登录过于频繁，请稍后重试",
		"auth.forbidden":           "权限不足",
		"csrf.invalid":             "CSRF 校验失败",
		"feature_disabled":         "功能已禁用",
		"execution.created":        "任务已创建",
		"execution.not_found":      "任务不存在",
		"server.list.success":      "服务器列表获取成功",
		"locale.updated":           "语言偏好已更新",
		"validation.failed":        "参数校验失败",
		"internal.error":           "内部错误",
	}
}

func defaultEN() map[string]string {
	return map[string]string{
		"ok":                       "ok",
		"auth.invalid_credentials": "invalid username or password",
		"auth.unauthorized":        "unauthorized",
		"auth.rate_limited":        "too many login attempts",
		"auth.forbidden":           "forbidden",
		"csrf.invalid":             "csrf validation failed",
		"feature_disabled":         "feature is disabled",
		"execution.created":        "execution created",
		"execution.not_found":      "execution not found",
		"server.list.success":      "server list fetched",
		"locale.updated":           "locale updated",
		"validation.failed":        "validation failed",
		"internal.error":           "internal error",
	}
}
