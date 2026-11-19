// Package seal is the reference implementation of the imgmesh content crypto.
// iOS and the web viewer must match it. The API never calls Seal with user photos.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	InfoManifest = "imgmesh-manifest-v1"
	InfoFile     = "imgmesh-file-v1"
	accessPrefix = "imgmesh-access-v1"
)

func Derive(master, shareID []byte, info string) ([]byte, error) {
	if len(master) != 32 || len(shareID) != 16 {
		return nil, errors.New("bad key length")
	}
	r := hkdf.New(sha256.New, master, shareID, []byte(info))
	out := make([]byte, 32)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	return out, nil
}

func AccessTag(master, shareID []byte) ([]byte, error) {
	if len(master) != 32 || len(shareID) != 16 {
		return nil, errors.New("bad key length")
	}
	m := hmac.New(sha256.New, master)
	m.Write([]byte(accessPrefix))
	m.Write(shareID)
	return m.Sum(nil), nil
}

func AAD(shareID, objectID []byte) ([]byte, error) {
	if len(shareID) != 16 || len(objectID) != 16 {
		return nil, errors.New("bad aad length")
	}
	out := make([]byte, 32)
	copy(out, shareID)
	copy(out[16:], objectID)
	return out, nil
}

// Encrypt returns ciphertext || 16-byte tag. nonce is stored separately.
func Encrypt(key, nonce, aad, plain []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 12 {
		return nil, errors.New("bad aes length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, nonce, plain, aad), nil
}

func Decrypt(key, nonce, aad, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 || len(nonce) != 12 {
		return nil, errors.New("bad aes length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}
