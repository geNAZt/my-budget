package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"
)

type CryptoService struct {
	serverID string
}

func NewCryptoService(dataDir string) (*CryptoService, error) {
	// Ensure directory exists
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}

	idPath := dataDir + "/server.id"

	// Try to read existing server ID
	data, err := os.ReadFile(idPath)
	if err == nil {
		return &CryptoService{serverID: string(data)}, nil
	}

	// Generate new server ID if not exists
	newID := uuid.New().String()
	if err := os.WriteFile(idPath, []byte(newID), 0600); err != nil {
		return nil, fmt.Errorf("failed to save server id: %v", err)
	}

	return &CryptoService{serverID: newID}, nil
}

// DeriveIdentityKey generates a stable 32-byte key for a specific authenticator bound to this server
func (s *CryptoService) DeriveIdentityKey(userPubKey []byte) ([]byte, error) {
	hash := sha256.New
	masterSecret := make([]byte, 0, len(s.serverID)+len(userPubKey))
	masterSecret = append(masterSecret, s.serverID...)
	masterSecret = append(masterSecret, userPubKey...)

	hkdf := hkdf.New(hash, masterSecret, []byte("IDENTITY_V1"), nil)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf, key); err != nil {
		return nil, err
	}
	return key, nil
}

// WrapKey encrypts a target key using a wrapping key
func (s *CryptoService) WrapKey(wrappingKey, targetKey []byte) ([]byte, error) {
	return s.Encrypt(wrappingKey, targetKey)
}

// UnwrapKey decrypts a target key using a wrapping key
func (s *CryptoService) UnwrapKey(wrappingKey, wrappedKey []byte) ([]byte, error) {
	return s.Decrypt(wrappingKey, wrappedKey)
}

// GenerateRandomKey creates a new 32-byte random key
func (s *CryptoService) GenerateRandomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *CryptoService) Encrypt(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func (s *CryptoService) Decrypt(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
