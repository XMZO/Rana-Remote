package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/ssh"
	"github.com/rana-remote/rana-remote/internal/task"
)

func main() {
	cfgPath := flag.String("c", "/data/config.yaml", "path to config file")
	timeoutOverride := flag.String("timeout", "", "override timeout")
	dryRun := flag.Bool("dry-run", false, "print generated script only")
	serversFlag := flag.String("server", "", "comma-separated server names")
	checkOnly := flag.Bool("check", false, "check ssh connectivity only")
	flag.Parse()

	cfg, _, err := config.LoadOrInit(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(2)
	}
	if strings.TrimSpace(*timeoutOverride) != "" {
		cfg.Global.Timeout = strings.TrimSpace(*timeoutOverride)
	}

	servers := cfg.FilterServers(splitCSV(*serversFlag))
	if len(servers) == 0 {
		fmt.Fprintln(os.Stderr, "no servers selected")
		os.Exit(2)
	}

	if *checkOnly {
		exitCode := runCheck(cfg, servers)
		os.Exit(exitCode)
	}

	if *dryRun {
		if err := runDry(cfg, servers); err != nil {
			fmt.Fprintf(os.Stderr, "dry-run: %v\n", err)
			os.Exit(2)
		}
		return
	}

	report := task.Run(context.Background(), cfg, servers, nil)
	printSummary(report)
	os.Exit(report.ExitCode())
}

func runCheck(cfg *config.Config, servers []config.Server) int {
	failed := 0
	for _, srv := range servers {
		client, err := sshclient.NewClient(srv, cfg.Global.SSH)
		if err != nil {
			failed++
			fmt.Printf("%s FAIL: %v\n", srv.Name, err)
			continue
		}
		_ = client.Close()
		fmt.Printf("%s OK\n", srv.Name)
	}
	if failed == 0 {
		return 0
	}
	if failed == len(servers) {
		return 2
	}
	return 1
}

func runDry(cfg *config.Config, servers []config.Server) error {
	for _, srv := range servers {
		script, err := task.GenerateScript(cfg.Global.TempDir, srv)
		if err != nil {
			return fmt.Errorf("server %s: %w", srv.Name, err)
		}
		fmt.Printf("===== %s =====\n%s\n", srv.Name, script)
	}
	return nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func printSummary(r task.Report) {
	fmt.Println("Summary")
	fmt.Println("-------")
	for _, res := range r.Results {
		status := "FAILED"
		if res.Success {
			status = "SUCCESS"
		}
		if res.Error != "" {
			fmt.Printf("%s\t%s\t%s\t%s\n", res.Server, status, res.Duration.Round(time.Millisecond), res.Error)
		} else {
			fmt.Printf("%s\t%s\t%s\n", res.Server, status, res.Duration.Round(time.Millisecond))
		}
	}
	fmt.Printf("Total duration: %s\n", r.Duration.Round(time.Millisecond))
}
