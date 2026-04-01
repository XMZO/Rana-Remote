package api



import (

	"net/http"

	"strings"

	"time"



	"github.com/rana-remote/rana-remote/internal/auth"

	"github.com/rana-remote/rana-remote/internal/config"

	"github.com/rana-remote/rana-remote/internal/store"

)



type updateSettingsRequest struct {

	Global  *updateGlobalSettings `json:"global,omitempty"`

	Modules *updateModuleSettings `json:"modules,omitempty"`

	Web     *updateWebSettings    `json:"web,omitempty"`

	I18N    *updateI18NSettings   `json:"i18n,omitempty"`

	Notify  *updateNotifySettings `json:"notify,omitempty"`

}



type updateGlobalSettings struct {

	Timeout     *string          `json:"timeout,omitempty"`

	Concurrency *int             `json:"concurrency,omitempty"`

	SSH         *updateSSHConfig `json:"ssh,omitempty"`

}



type updateSSHConfig struct {

	StrictHostKey  *bool   `json:"strict_host_key,omitempty"`

	KnownHostsPath *string `json:"known_hosts_path,omitempty"`

}



type updateModuleSettings struct {

	Schedule *bool `json:"schedule,omitempty"`

	Audit    *bool `json:"audit,omitempty"`

	Notify   *bool `json:"notify,omitempty"`

	Users    *bool `json:"users,omitempty"`

}



type updateWebSettings struct {

	CSRFEnabled      *bool    `json:"csrf_enabled,omitempty"`

	CORSAllowOrigins []string `json:"cors_allow_origins,omitempty"`

	IPAllowList      []string `json:"ip_allow_list,omitempty"`

}



type updateI18NSettings struct {

	DefaultLocale *string `json:"default_locale,omitempty"`

}



type updateEmailNotifySettings struct {

	Enabled  *bool    `json:"enabled,omitempty"`

	SMTPHost *string  `json:"smtp_host,omitempty"`

	SMTPPort *int     `json:"smtp_port,omitempty"`

	Username *string  `json:"username,omitempty"`

	Password *string  `json:"password,omitempty"`

	From     *string  `json:"from,omitempty"`

	To       []string `json:"to,omitempty"`

	UseTLS   *bool    `json:"use_tls,omitempty"`

}



type updateNotifySettings struct {

	WebhookURL        *string                    `json:"webhook_url,omitempty"`

	Email             *updateEmailNotifySettings `json:"email,omitempty"`

	OnSuccess         *bool                      `json:"on_success,omitempty"`

	OnFailure         *bool                      `json:"on_failure,omitempty"`

	SuppressionWindow *string                    `json:"suppression_window,omitempty"`

}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeJSON(w, http.StatusOK, map[string]any{
			"code": "ok",
			"data": map[string]any{
				"global": map[string]any{
					"timeout":     s.cfg.Global.Timeout,
					"concurrency": s.cfg.Global.Concurrency,
					"ssh": map[string]any{
						"strict_host_key":  s.cfg.Global.SSH.StrictHostKey,
						"known_hosts_path": s.cfg.Global.SSH.KnownHostsPath,
					},
				},
				"web": map[string]any{
					"csrf_enabled":       s.cfg.Web.CSRFEnabled,
					"cors_allow_origins": append([]string(nil), s.cfg.Web.CORSAllowOrigins...),
					"ip_allow_list":      append([]string(nil), s.cfg.Web.IPAllowList...),
				},
				"modules": map[string]any{
					"backup":   s.cfg.Modules.Backup,
					"schedule": s.cfg.Modules.Schedule,
					"audit":    s.cfg.Modules.Audit,
					"notify":   s.cfg.Modules.Notify,
					"users":    s.cfg.Modules.Users,
				},
				"i18n": map[string]any{
					"default_locale":      s.cfg.I18N.DefaultLocale,
					"supported_locales":   append([]string(nil), s.cfg.I18N.SupportedLocales...),
					"fallback_locale":     s.cfg.I18N.FallbackLocale,
					"locale_source_order": append([]string(nil), s.cfg.I18N.LocaleSourceOrder...),
				},
				"notify": map[string]any{
					"webhook_url": s.cfg.Notify.WebhookURL,
					"email": map[string]any{
						"enabled":   s.cfg.Notify.Email.Enabled,
						"smtp_host": s.cfg.Notify.Email.SMTPHost,
						"smtp_port": s.cfg.Notify.Email.SMTPPort,
						"username":  s.cfg.Notify.Email.Username,
						"password":  s.cfg.Notify.Email.Password,
						"from":      s.cfg.Notify.Email.From,
						"to":        append([]string(nil), s.cfg.Notify.Email.To...),
						"use_tls":   s.cfg.Notify.Email.UseTLS,
					},
					"on_success":         s.cfg.Notify.OnSuccess,
					"on_failure":         s.cfg.Notify.OnFailure,
					"suppression_window": s.cfg.Notify.SuppressionWindow,
				},
			},
		})
	case http.MethodPut:
		var req updateSettingsRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		original := *s.cfg
		next := original
		if req.Global != nil {
			if req.Global.Timeout != nil {
				next.Global.Timeout = strings.TrimSpace(*req.Global.Timeout)
			}
			if req.Global.Concurrency != nil {
				next.Global.Concurrency = *req.Global.Concurrency
			}
			if req.Global.SSH != nil {
				if req.Global.SSH.StrictHostKey != nil {
					next.Global.SSH.StrictHostKey = *req.Global.SSH.StrictHostKey
				}
				if req.Global.SSH.KnownHostsPath != nil {
					next.Global.SSH.KnownHostsPath = strings.TrimSpace(*req.Global.SSH.KnownHostsPath)
				}
			}
		}
		if req.Modules != nil {
			if req.Modules.Schedule != nil {
				next.Modules.Schedule = *req.Modules.Schedule
			}
			if req.Modules.Audit != nil {
				next.Modules.Audit = *req.Modules.Audit
			}
			if req.Modules.Notify != nil {
				next.Modules.Notify = *req.Modules.Notify
			}
			if req.Modules.Users != nil {
				next.Modules.Users = *req.Modules.Users
			}
		}
		if req.Web != nil {
			if req.Web.CSRFEnabled != nil {
				next.Web.CSRFEnabled = *req.Web.CSRFEnabled
			}
			if req.Web.CORSAllowOrigins != nil {
				next.Web.CORSAllowOrigins = append([]string(nil), req.Web.CORSAllowOrigins...)
			}
			if req.Web.IPAllowList != nil {
				next.Web.IPAllowList = append([]string(nil), req.Web.IPAllowList...)
			}
		}
		if req.I18N != nil && req.I18N.DefaultLocale != nil {
			next.I18N.DefaultLocale = strings.TrimSpace(*req.I18N.DefaultLocale)
		}
		if req.Notify != nil {
			if req.Notify.WebhookURL != nil {
				next.Notify.WebhookURL = strings.TrimSpace(*req.Notify.WebhookURL)
			}
			if req.Notify.Email != nil {
				if req.Notify.Email.Enabled != nil {
					next.Notify.Email.Enabled = *req.Notify.Email.Enabled
				}
				if req.Notify.Email.SMTPHost != nil {
					next.Notify.Email.SMTPHost = strings.TrimSpace(*req.Notify.Email.SMTPHost)
				}
				if req.Notify.Email.SMTPPort != nil {
					next.Notify.Email.SMTPPort = *req.Notify.Email.SMTPPort
				}
				if req.Notify.Email.Username != nil {
					next.Notify.Email.Username = strings.TrimSpace(*req.Notify.Email.Username)
				}
				if req.Notify.Email.Password != nil {
					next.Notify.Email.Password = strings.TrimSpace(*req.Notify.Email.Password)
				}
				if req.Notify.Email.From != nil {
					next.Notify.Email.From = strings.TrimSpace(*req.Notify.Email.From)
				}
				if req.Notify.Email.To != nil {
					next.Notify.Email.To = append([]string(nil), req.Notify.Email.To...)
				}
				if req.Notify.Email.UseTLS != nil {
					next.Notify.Email.UseTLS = *req.Notify.Email.UseTLS
				}
			}
			if req.Notify.OnSuccess != nil {
				next.Notify.OnSuccess = *req.Notify.OnSuccess
			}
			if req.Notify.OnFailure != nil {
				next.Notify.OnFailure = *req.Notify.OnFailure
			}
			if req.Notify.SuppressionWindow != nil {
				next.Notify.SuppressionWindow = strings.TrimSpace(*req.Notify.SuppressionWindow)
			}
		}
		next = config.WithDefaults(next)
		if err := (&next).Validate(); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		*s.cfg = next

		// Persist to database instead of config.yaml
		dbSettings := store.SystemSettings{
			GlobalTimeout:           next.Global.Timeout,
			GlobalConcurrency:       next.Global.Concurrency,
			GlobalSSHStrictHostKey: next.Global.SSH.StrictHostKey,
			GlobalSSHKnownHostsPath: next.Global.SSH.KnownHostsPath,
			WebCSRFEnabled:          next.Web.CSRFEnabled,
			WebCORSAllowOrigins:     append([]string(nil), next.Web.CORSAllowOrigins...),
			WebIPAllowList:          append([]string(nil), next.Web.IPAllowList...),
			ModulesSchedule:        next.Modules.Schedule,
			ModulesAudit:           next.Modules.Audit,
			ModulesNotify:          next.Modules.Notify,
			ModulesUsers:           next.Modules.Users,
			I18NDefaultLocale:      next.I18N.DefaultLocale,
			NotifyWebhookURL:        next.Notify.WebhookURL,
			NotifyOnSuccess:        next.Notify.OnSuccess,
			NotifyOnFailure:        next.Notify.OnFailure,
			NotifySuppressionWindow: next.Notify.SuppressionWindow,
			NotifyEmailEnabled:     next.Notify.Email.Enabled,
			NotifyEmailSMTPHost:    next.Notify.Email.SMTPHost,
			NotifyEmailSMTPPort:    next.Notify.Email.SMTPPort,
			NotifyEmailUsername:    next.Notify.Email.Username,
			NotifyEmailPassword:    next.Notify.Email.Password,
			NotifyEmailFrom:        next.Notify.Email.From,
			NotifyEmailTo:          append([]string(nil), next.Notify.Email.To...),
			NotifyEmailUseTLS:      next.Notify.Email.UseTLS,
			UpdatedAt:              time.Now().UTC(),
		}
		if err := s.repo.SaveSettings(r.Context(), dbSettings); err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", map[string]any{"detail": err.Error()})
			return
		}

		if actor, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
				ActorID:      actor.ID,
				Action:       "settings.update",
				ResourceType: "settings",
				ResourceID:   "global",
				IP:           remoteIP(r),
				Diff:         "trace_id=" + traceIDFromContext(r.Context()),
			}))
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": "updated"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
