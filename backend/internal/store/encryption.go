package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

// Encryptor provides AES-256-GCM encryption/decryption for sensitive data.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor creates an Encryptor from a hex-encoded 32-byte key.
func NewEncryptor(hexKey string) (*Encryptor, error) {
	if hexKey == "" {
		return nil, errors.New("encryption key is required")
	}

	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, errors.New("invalid encryption key: must be hex-encoded")
	}

	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes (64 hex chars)")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Encryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext and returns hex-encoded ciphertext.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decrypts hex-encoded ciphertext and returns plaintext.
func (e *Encryptor) Decrypt(hexCiphertext string) (string, error) {
	ciphertext, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", err
	}

	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
