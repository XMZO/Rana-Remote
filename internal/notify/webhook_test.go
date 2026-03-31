package notify

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/store"
)

func TestWebhookNotifier_Failure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(&config.NotifyConfig{
		WebhookURL:        srv.URL,
		OnFailure:         true,
		OnSuccess:         false,
		SuppressionWindow: "0s",
	})
	err := n.NotifyExecution(t.Context(), store.Execution{
		ID:     "ex-1",
		Status: store.StatusFailed,
	})
	if err != nil {
		t.Fatalf("notify failure: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 webhook call, got %d", hits.Load())
	}
}

func TestWebhookNotifier_Suppression(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(&config.NotifyConfig{
		WebhookURL:        srv.URL,
		OnFailure:         true,
		SuppressionWindow: "1h",
	})
	_ = n.NotifyExecution(t.Context(), store.Execution{ID: "a", Status: store.StatusFailed})
	_ = n.NotifyExecution(t.Context(), store.Execution{ID: "b", Status: store.StatusFailed})
	if hits.Load() != 1 {
		t.Fatalf("expected suppression to keep one call, got %d", hits.Load())
	}

	n.lastFailure = time.Now().UTC().Add(-2 * time.Hour)
	_ = n.NotifyExecution(t.Context(), store.Execution{ID: "c", Status: store.StatusFailed})
	if hits.Load() != 2 {
		t.Fatalf("expected call after window, got %d", hits.Load())
	}
}

func TestWebhookNotifier_EmailTLS(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{mustSelfSignedCert(t)},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("listen tls: %v", err)
	}
	defer ln.Close()

	var mailFrom string
	var rcpts []string
	var gotData strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		writeLine := func(format string, args ...any) {
			_, _ = fmt.Fprintf(w, format+"\r\n", args...)
			_ = w.Flush()
		}
		writeLine("220 localhost ESMTP")
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					writeLine("250 queued")
					inData = false
					continue
				}
				gotData.WriteString(line)
				gotData.WriteString("\n")
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO "):
				writeLine("250-localhost")
				writeLine("250 AUTH PLAIN")
			case strings.HasPrefix(line, "AUTH PLAIN "):
				writeLine("235 authenticated")
			case strings.HasPrefix(line, "MAIL FROM:"):
				mailFrom = strings.TrimPrefix(line, "MAIL FROM:")
				writeLine("250 ok")
			case strings.HasPrefix(line, "RCPT TO:"):
				rcpts = append(rcpts, strings.TrimPrefix(line, "RCPT TO:"))
				writeLine("250 ok")
			case line == "DATA":
				writeLine("354 end data with <CR><LF>.<CR><LF>")
				inData = true
			case line == "QUIT":
				writeLine("221 bye")
				return
			default:
				writeLine("250 ok")
			}
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port := 0
	fmt.Sscanf(portStr, "%d", &port)
	n := NewWebhookNotifier(&config.NotifyConfig{
		Email: config.EmailNotifyConfig{
			Enabled:  true,
			SMTPHost: host,
			SMTPPort: port,
			Username: "bot",
			Password: "secret",
			From:     "rana@example.com",
			To:       []string{"ops@example.com"},
			UseTLS:   true,
		},
		OnFailure: true,
	})
	n.client = &http.Client{Timeout: time.Second}

	if err := n.NotifyExecution(t.Context(), store.Execution{ID: "ex-1", Status: store.StatusFailed}); err != nil {
		t.Fatalf("notify email tls: %v", err)
	}
	<-done
	if !strings.Contains(mailFrom, "rana@example.com") {
		t.Fatalf("unexpected MAIL FROM: %q", mailFrom)
	}
	if len(rcpts) != 1 || !strings.Contains(rcpts[0], "ops@example.com") {
		t.Fatalf("unexpected rcpts: %+v", rcpts)
	}
	if !strings.Contains(gotData.String(), "Subject: [Rana] Execution failed") {
		t.Fatalf("expected subject in payload, got: %s", gotData.String())
	}
}
