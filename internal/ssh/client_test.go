package sshclient

import (
	"testing"

	"github.com/rana-remote/rana-remote/internal/config"
)

func TestHostKeyCallback_Insecure(t *testing.T) {
	cb, err := hostKeyCallback(config.SSHConfig{StrictHostKey: false}, "host")
	if err != nil {
		t.Fatalf("hostKeyCallback error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected callback")
	}
}

func TestHostKeyCallback_StrictMissingFile(t *testing.T) {
	_, err := hostKeyCallback(config.SSHConfig{StrictHostKey: true, KnownHostsPath: "nope"}, "host")
	if err == nil {
		t.Fatal("expected error")
	}
}
