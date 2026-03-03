package task

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/config"
)

type fakeExecutor struct {
	err error
}

func (f fakeExecutor) Execute(_ context.Context, _ config.Server, _ string, _, _ io.Writer) error {
	return f.err
}

func TestGenerateScriptQuotes(t *testing.T) {
	srv := config.Server{
		Name:  "node",
		Paths: []string{"/opt/a b"},
		Rclone: config.Rclone{
			Remote: "s3:bucket path",
			Flags:  []string{"--header", "x y"},
		},
	}
	s, err := GenerateScript("/tmp/rana backup", srv)
	if err != nil {
		t.Fatalf("GenerateScript error: %v", err)
	}
	if !strings.Contains(s, "'/tmp/rana backup'") {
		t.Fatalf("temp dir not quoted: %s", s)
	}
	if !strings.Contains(s, "'/opt/a b'") {
		t.Fatalf("path not quoted: %s", s)
	}
}

func TestRunReportExitCode(t *testing.T) {
	cfg := &config.Config{Global: config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}}}
	servers := []config.Server{{Name: "a", Paths: []string{"/etc"}, Rclone: config.Rclone{Remote: "s3:x"}}}
	report := Run(context.Background(), cfg, servers, fakeExecutor{err: errors.New("boom")})
	if report.ExitCode() != 2 {
		t.Fatalf("unexpected exit code: %d", report.ExitCode())
	}
	if len(report.Results) != 1 || report.Results[0].Success {
		t.Fatalf("unexpected result: %+v", report.Results)
	}
}

func TestExitCodeMix(t *testing.T) {
	r := Report{Results: []Result{{Success: true}, {Success: false}}}
	if r.ExitCode() != 1 {
		t.Fatalf("expected 1, got %d", r.ExitCode())
	}
}

func TestRunHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := &config.Config{Global: config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", Concurrency: 1}}
	servers := []config.Server{{Name: "a", Paths: []string{"/etc"}, Rclone: config.Rclone{Remote: "s3:x"}}}
	report := Run(ctx, cfg, servers, fakeExecutor{})
	if len(report.Results) != 1 {
		t.Fatalf("expected one result")
	}
	if report.Results[0].Success {
		t.Fatalf("expected failure on canceled context")
	}
	if report.Duration > time.Second {
		t.Fatalf("expected quick return")
	}
}
