// Package id encodes the fixed-width identifiers from docs/architecture.md.
package id

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

var (
	ErrID = errors.New("invalid id")
)

// Encode is base64url without padding.
func Encode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeFixed decodes canonical base64url of exactly n bytes.
func DecodeFixed(s string, n int) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != n || Encode(b) != s {
		return nil, ErrID
	}
	return b, nil
}

func Decode16(s string) ([]byte, error) { return DecodeFixed(s, 16) }
func Decode32(s string) ([]byte, error) { return DecodeFixed(s, 32) }
func Decode12(s string) ([]byte, error) { return DecodeFixed(s, 12) }

func Random(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("random: %w", err)
	}
	return b, nil
}
