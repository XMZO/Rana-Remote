package task

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/logger"
	sshclient "github.com/rana-remote/rana-remote/internal/ssh"
)

// Report contains execution summary.
type Report struct {
	Results  []Result      `json:"results"`
	Duration time.Duration `json:"duration"`
}

// Result is per-server execution status.
type Result struct {
	Server   string        `json:"server"`
	Success  bool          `json:"success"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// Executor executes rendered script on a server.
type Executor interface {
	Execute(ctx context.Context, srv config.Server, script string, stdout, stderr io.Writer) error
}

type SSHExecutor struct {
	SSHConfig config.SSHConfig
}

func (e SSHExecutor) Execute(ctx context.Context, srv config.Server, script string, stdout, stderr io.Writer) error {
	client, err := sshclient.NewClient(srv, e.SSHConfig)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.RunScript(ctx, script, stdout, stderr)
}

func (r *Report) ExitCode() int {
	if len(r.Results) == 0 {
		return 0
	}
	failed := 0
	for _, result := range r.Results {
		if !result.Success {
			failed++
		}
	}
	if failed == 0 {
		return 0
	}
	if failed == len(r.Results) {
		return 2
	}
	return 1
}

// Run executes backup on selected servers with configured concurrency.
func Run(ctx context.Context, cfg *config.Config, servers []config.Server, executor Executor) Report {
	if executor == nil {
		executor = SSHExecutor{SSHConfig: cfg.Global.SSH}
	}
	if len(servers) == 0 {
		servers = cfg.Servers
	}
	start := time.Now()

	results := make([]Result, 0, len(servers))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	concurrency := cfg.Global.Concurrency
	if concurrency < 0 {
		concurrency = 0
	}
	var sem chan struct{}
	if concurrency > 0 {
		sem = make(chan struct{}, concurrency)
	}

	for _, srv := range servers {
		srv := srv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sem != nil {
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					appendResult(&mu, &results, Result{Server: srv.Name, Success: false, Error: contextError(ctx), Duration: 0})
					return
				}
				defer func() { <-sem }()
			}

			res := runOne(ctx, cfg, srv, executor)
			appendResult(&mu, &results, res)
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool {
		return results[i].Server < results[j].Server
	})

	return Report{Results: results, Duration: time.Since(start)}
}

func runOne(parent context.Context, cfg *config.Config, srv config.Server, executor Executor) Result {
	start := time.Now()
	if parent.Err() != nil {
		return Result{Server: srv.Name, Success: false, Duration: 0, Error: contextError(parent)}
	}
	logger.Info(srv.Name, "backup started")
	serverCtx := parent
	if timeout, err := cfg.GlobalTimeout(); err == nil && timeout > 0 {
		var cancel context.CancelFunc
		serverCtx, cancel = context.WithTimeout(parent, timeout)
		defer cancel()
	}

	script, err := GenerateScript(cfg.Global.TempDir, srv)
	if err != nil {
		msg := fmt.Sprintf("generate script: %v", err)
		logger.Error(srv.Name, "%s", msg)
		return Result{Server: srv.Name, Success: false, Duration: time.Since(start), Error: msg}
	}

	err = executor.Execute(serverCtx, srv, script, logger.PrefixWriter(srv.Name, "INFO"), logger.PrefixWriter(srv.Name, "ERROR"))
	if err != nil {
		msg := err.Error()
		logger.Error(srv.Name, "backup failed: %s", msg)
		return Result{Server: srv.Name, Success: false, Duration: time.Since(start), Error: msg}
	}

	logger.Info(srv.Name, "backup finished")
	return Result{Server: srv.Name, Success: true, Duration: time.Since(start)}
}

func appendResult(mu *sync.Mutex, results *[]Result, r Result) {
	mu.Lock()
	defer mu.Unlock()
	*results = append(*results, r)
}

func contextError(ctx context.Context) string {
	if err := context.Cause(ctx); err != nil {
		return err.Error()
	}
	if err := ctx.Err(); err != nil {
		return err.Error()
	}
	return "context canceled"
}
