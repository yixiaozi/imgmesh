package blob

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

const tokenVersion byte = 1

var errToken = errors.New("invalid blob token")

// Sign builds a short-lived capability for one object key.
// part 0 is the whole object. method is 'G' or 'P'.
func Sign(secret []byte, method byte, key string, exp time.Time, maxBytes int64, part uint16) (string, error) {
	if method != 'G' && method != 'P' {
		return "", errToken
	}
	payload := encodePayload(method, key, exp, maxBytes, part)
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

type Ticket struct {
	Method   byte
	Key      string
	Expires  time.Time
	MaxBytes int64
	Part     uint16
}

func Verify(secret []byte, token string, now time.Time) (Ticket, error) {
	left, right, ok := strings.Cut(token, ".")
	if !ok {
		return Ticket{}, errToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(left)
	if err != nil {
		return Ticket{}, errToken
	}
	mac, err := base64.RawURLEncoding.DecodeString(right)
	if err != nil || len(mac) != sha256.Size {
		return Ticket{}, errToken
	}
	h := hmac.New(sha256.New, secret)
	h.Write(payload)
	if !hmac.Equal(mac, h.Sum(nil)) {
		return Ticket{}, errToken
	}
	t, err := decodePayload(payload)
	if err != nil {
		return Ticket{}, err
	}
	if !t.Expires.After(now) {
		return Ticket{}, errToken
	}
	return t, nil
}

func encodePayload(method byte, key string, exp time.Time, maxBytes int64, part uint16) []byte {
	keyb := []byte(key)
	buf := make([]byte, 0, 1+1+8+8+2+2+len(keyb))
	buf = append(buf, tokenVersion, method)
	buf = binary.BigEndian.AppendUint64(buf, uint64(exp.Unix()))
	buf = binary.BigEndian.AppendUint64(buf, uint64(maxBytes))
	buf = binary.BigEndian.AppendUint16(buf, part)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(keyb)))
	buf = append(buf, keyb...)
	return buf
}

func decodePayload(b []byte) (Ticket, error) {
	if len(b) < 1+1+8+8+2+2 || b[0] != tokenVersion {
		return Ticket{}, errToken
	}
	method := b[1]
	if method != 'G' && method != 'P' {
		return Ticket{}, errToken
	}
	exp := int64(binary.BigEndian.Uint64(b[2:10]))
	maxBytes := int64(binary.BigEndian.Uint64(b[10:18]))
	part := binary.BigEndian.Uint16(b[18:20])
	keyLen := int(binary.BigEndian.Uint16(b[20:22]))
	if keyLen < 0 || 22+keyLen != len(b) {
		return Ticket{}, errToken
	}
	return Ticket{
		Method:   method,
		Key:      string(b[22:]),
		Expires:  time.Unix(exp, 0),
		MaxBytes: maxBytes,
		Part:     part,
	}, nil
}
