package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SecretBox struct { aead cipher.AEAD }

func LoadOrCreateSecretBox(path string) (*SecretBox, error) {
	key, _, err := loadOrCreateRandom(path, 32)
	if err != nil { return nil, err }
	block, err := aes.NewCipher(key)
	if err != nil { return nil, err }
	aead, err := cipher.NewGCM(block)
	if err != nil { return nil, err }
	return &SecretBox{aead: aead}, nil
}

func (s *SecretBox) Encrypt(value string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil { return "", err }
	sealed := s.aead.Seal(nil, nonce, []byte(value), nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func (s *SecretBox) Decrypt(encoded string) (string, error) {
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil { return "", err }
	ns := s.aead.NonceSize()
	if len(payload) < ns { return "", errors.New("invalid encrypted payload") }
	clear, err := s.aead.Open(nil, payload[:ns], payload[ns:], nil)
	if err != nil { return "", err }
	return string(clear), nil
}

func LoadOrCreateAdminToken(path string) (string, bool, error) {
	raw, created, err := loadOrCreateRandom(path, 32)
	if err != nil { return "", false, err }
	return base64.RawURLEncoding.EncodeToString(raw), created, nil
}

func loadOrCreateRandom(path string, size int) ([]byte, bool, error) {
	if data, err := os.ReadFile(path); err == nil {
		decoded, decErr := base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decErr != nil { return nil, false, fmt.Errorf("decode %s: %w", path, decErr) }
		if len(decoded) != size { return nil, false, fmt.Errorf("%s has invalid size", path) }
		return decoded, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { return nil, false, err }
	raw := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil { return nil, false, err }
	content := base64.RawStdEncoding.EncodeToString(raw) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { return nil, false, err }
	if err := os.Chmod(path, 0o600); err != nil { return nil, false, err }
	return raw, true, nil
}
