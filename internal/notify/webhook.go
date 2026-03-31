package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/store"
)

type WebhookNotifier struct {
	cfg         *config.NotifyConfig
	client      *http.Client
	mu          sync.Mutex
	lastFailure time.Time
}

func NewWebhookNotifier(cfg *config.NotifyConfig) *WebhookNotifier {
	if cfg == nil {
		cfg = &config.NotifyConfig{}
	}
	return &WebhookNotifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *WebhookNotifier) NotifyExecution(ctx context.Context, ex store.Execution) error {
	if n == nil || n.cfg == nil {
		return nil
	}
	isSuccess := ex.Status == store.StatusSuccess
	isFailure := ex.Status == store.StatusFailed
	if isSuccess && !n.cfg.OnSuccess {
		return nil
	}
	if isFailure && !n.cfg.OnFailure {
		return nil
	}
	if !isSuccess && !isFailure {
		return nil
	}
	if isFailure && !n.allowFailureSend() {
		return nil
	}

	payload := map[string]any{
		"type":         "execution",
		"execution_id": ex.ID,
		"status":       ex.Status,
		"policy_id":    ex.PolicyID,
		"trigger_type": ex.TriggerType,
		"server_names": ex.ServerNames,
		"started_at":   ex.StartedAt,
		"ended_at":     ex.EndedAt,
		"duration_ms":  ex.DurationMS,
		"error":        ex.Error,
		"results":      ex.Results,
	}

	var errs []string
	if hook := strings.TrimSpace(n.cfg.WebhookURL); hook != "" {
		if err := n.sendWebhook(ctx, hook, payload); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if n.cfg.Email.Enabled {
		if err := n.sendEmail(payload); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (n *WebhookNotifier) sendWebhook(ctx context.Context, hook string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook notify status=%d", resp.StatusCode)
	}
	return nil
}

func (n *WebhookNotifier) sendEmail(payload map[string]any) error {
	cfg := n.cfg.Email
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	subject := fmt.Sprintf("[Rana] Execution %v", payload["status"])
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	msg := bytes.NewBuffer(nil)
	fmt.Fprintf(msg, "From: %s\r\n", cfg.From)
	fmt.Fprintf(msg, "To: %s\r\n", strings.Join(cfg.To, ", "))
	fmt.Fprintf(msg, "Subject: %s\r\n", subject)
	fmt.Fprintf(msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(msg, "Content-Type: application/json; charset=UTF-8\r\n\r\n")
	msg.Write(body)

	var auth smtp.Auth
	if strings.TrimSpace(cfg.Username) != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)
	}
	if cfg.UseTLS {
		tlsCfg := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}
		if net.ParseIP(cfg.SMTPHost) != nil {
			tlsCfg.InsecureSkipVerify = true
		}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, cfg.SMTPHost)
		if err != nil {
			return err
		}
		defer client.Close()
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		if err := client.Mail(cfg.From); err != nil {
			return err
		}
		for _, to := range cfg.To {
			if err := client.Rcpt(to); err != nil {
				return err
			}
		}
		wc, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := wc.Write(msg.Bytes()); err != nil {
			_ = wc.Close()
			return err
		}
		if err := wc.Close(); err != nil {
			return err
		}
		return client.Quit()
	}
	return smtp.SendMail(addr, auth, cfg.From, cfg.To, msg.Bytes())
}

func (n *WebhookNotifier) allowFailureSend() bool {
	if n == nil || n.cfg == nil {
		return false
	}
	window, err := time.ParseDuration(strings.TrimSpace(n.cfg.SuppressionWindow))
	if err != nil || window <= 0 {
		return true
	}
	now := time.Now().UTC()
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.lastFailure.IsZero() && now.Sub(n.lastFailure) < window {
		return false
	}
	n.lastFailure = now
	return true
}
