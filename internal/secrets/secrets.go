package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

const prefix = "enc:v1:"

type Cipher struct {
	aead cipher.AEAD
}

func New(key string) (*Cipher, error) {
	k, err := parseKey(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func parseKey(s string) ([]byte, error) {
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("secret key must be 32 bytes (hex or base64)")
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

func (c *Cipher) Decrypt(stored string) (string, error) {
	if stored == "" || len(stored) < len(prefix) || stored[:len(prefix)] != prefix {
		return stored, nil
	}
	data, err := base64.StdEncoding.DecodeString(stored[len(prefix):])
	if err != nil {
		return "", err
	}
	ns := c.aead.NonceSize()
	if len(data) < ns {
		return "", errors.New("invalid ciphertext")
	}
	nonce, ct := data[:ns], data[ns:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
