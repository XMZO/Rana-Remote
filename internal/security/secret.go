package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	dataKeyEnv    = "RANA_DATA_KEY"
	encPrefixV1   = "enc:v1:"
	nonceByteSize = 12
)

var (
	loadKeyOnce sync.Once
	keyErr      error
	blockCipher cipher.AEAD
)

// EncryptIfConfigured encrypts plaintext with RANA_DATA_KEY.
// When key is not configured, plaintext is returned unchanged.
func EncryptIfConfigured(plaintext string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return plaintext, nil
	}
	if strings.HasPrefix(plaintext, encPrefixV1) {
		return plaintext, nil
	}
	gcm, err := getCipher()
	if err != nil {
		return "", err
	}
	if gcm == nil {
		return plaintext, nil
	}
	nonce := make([]byte, nonceByteSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	encrypted := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(nonce, encrypted...)
	return encPrefixV1 + base64.StdEncoding.EncodeToString(out), nil
}

// DecryptIfConfigured decrypts a value encrypted by EncryptIfConfigured.
// Plaintext values are returned as-is.
func DecryptIfConfigured(value string) (string, error) {
	if !strings.HasPrefix(value, encPrefixV1) {
		return value, nil
	}
	gcm, err := getCipher()
	if err != nil {
		return "", err
	}
	if gcm == nil {
		return "", errors.New("RANA_DATA_KEY is required to decrypt secrets")
	}
	raw := strings.TrimPrefix(value, encPrefixV1)
	payload, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", err
	}
	if len(payload) <= nonceByteSize {
		return "", errors.New("invalid encrypted payload")
	}
	nonce := payload[:nonceByteSize]
	cipherText := payload[nonceByteSize:]
	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func getCipher() (cipher.AEAD, error) {
	loadKeyOnce.Do(func() {
		secret := strings.TrimSpace(os.Getenv(dataKeyEnv))
		if secret == "" {
			blockCipher = nil
			return
		}
		key := deriveKey(secret)
		block, err := aes.NewCipher(key)
		if err != nil {
			keyErr = err
			return
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			keyErr = err
			return
		}
		blockCipher = gcm
	})
	return blockCipher, keyErr
}

func deriveKey(secret string) []byte {
	if decodedHex, err := hex.DecodeString(secret); err == nil && len(decodedHex) == 32 {
		return decodedHex
	}
	if decodedB64, err := base64.StdEncoding.DecodeString(secret); err == nil && len(decodedB64) == 32 {
		return decodedB64
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}
