package security

import (
	"strings"
	"sync"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	t.Setenv(dataKeyEnv, strings.Repeat("a", 32))
	resetForTest()

	raw := "hello-secret"
	enc, err := EncryptIfConfigured(raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == raw {
		t.Fatalf("expected encrypted value")
	}
	dec, err := DecryptIfConfigured(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if dec != raw {
		t.Fatalf("unexpected decrypt result: %q", dec)
	}
}

func TestPlaintextWhenNoKey(t *testing.T) {
	t.Setenv(dataKeyEnv, "")
	resetForTest()

	raw := "plain"
	enc, err := EncryptIfConfigured(raw)
	if err != nil {
		t.Fatalf("encrypt no key: %v", err)
	}
	if enc != raw {
		t.Fatalf("expected plaintext when key missing")
	}
}

func resetForTest() {
	loadKeyOnce = sync.Once{}
	keyErr = nil
	blockCipher = nil
}
