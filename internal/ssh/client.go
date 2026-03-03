package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/logger"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	connectTimeout = 10 * time.Second
	killWait       = 5 * time.Second
)

// Client wraps SSH client.
type Client struct {
	client *ssh.Client
	host   string
}

func NewClient(srv config.Server, sshCfg config.SSHConfig) (*Client, error) {
	auth, err := privateKeyAuth(srv)
	if err != nil {
		return nil, err
	}

	hostKey, err := hostKeyCallback(sshCfg, srv.Name)
	if err != nil {
		return nil, err
	}

	clientCfg := &ssh.ClientConfig{
		User:            srv.User,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: hostKey,
		Timeout:         connectTimeout,
	}

	addr := net.JoinHostPort(srv.Host, fmt.Sprintf("%d", srv.Port))
	c, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return &Client{client: c, host: srv.Name}, nil
}

func privateKeyAuth(srv config.Server) (ssh.AuthMethod, error) {
	key, err := os.ReadFile(srv.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	var signer ssh.Signer
	if strings.TrimSpace(srv.Passphrase) == "" {
		signer, err = ssh.ParsePrivateKey(key)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(srv.Passphrase))
	}
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return ssh.PublicKeys(signer), nil
}

func hostKeyCallback(cfg config.SSHConfig, host string) (ssh.HostKeyCallback, error) {
	if cfg.StrictHostKey {
		cb, err := knownhosts.New(cfg.KnownHostsPath)
		if err != nil {
			return nil, fmt.Errorf("init known_hosts: %w", err)
		}
		return cb, nil
	}
	logger.Warn(host, "strict_host_key disabled, using insecure host key callback")
	return ssh.InsecureIgnoreHostKey(), nil
}

func (c *Client) RunScript(ctx context.Context, script string, stdout, stderr io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	in, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	out, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	errOut, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := session.Start("bash -s"); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	go copyStream(out, stdout)
	go copyStream(errOut, stderr)

	writeDone := make(chan error, 1)
	go func() {
		_, wErr := io.WriteString(in, script)
		_ = in.Close()
		writeDone <- wErr
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- session.Wait() }()

	select {
	case err := <-waitDone:
		if wErr := <-writeDone; wErr != nil {
			return fmt.Errorf("write remote script: %w", wErr)
		}
		if err != nil {
			return fmt.Errorf("remote script failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		select {
		case <-waitDone:
			return contextErr(ctx)
		case <-time.After(killWait):
			_ = session.Close()
			_ = c.client.Close()
			return contextErr(ctx)
		}
	}
}

func copyStream(src io.Reader, dst io.Writer) {
	if dst == nil {
		_, _ = io.Copy(io.Discard, src)
		return
	}
	_, _ = io.Copy(dst, src)
}

func contextErr(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return fmt.Errorf("execution canceled: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("execution canceled: %w", err)
	}
	return errors.New("execution canceled")
}

func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}
