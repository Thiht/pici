package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

const prefix = "enc:v1:"

type Cipher struct {
	aead cipher.AEAD
}

func New(key string) (*Cipher, error) {
	k, err := hex.DecodeString(key)
	if err != nil || len(k) != 32 {
		k, err = base64.StdEncoding.DecodeString(key)
		if err != nil || len(k) != 32 {
			return nil, errors.New("secret key must be 32 bytes (hex or base64)")
		}
	}

	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}

	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	ct := c.aead.Seal(nil, nil, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

func (c *Cipher) Decrypt(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", err
	}

	pt, err := c.aead.Open(nil, nil, data, nil)
	if err != nil {
		return "", err
	}

	return string(pt), nil
}
