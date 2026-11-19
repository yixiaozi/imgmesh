package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifyAppleIdentityToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "test-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{"kid": kid, "kty": "RSA", "alg": "RS256", "n": n, "e": e}},
		})
	}))
	defer srv.Close()

	claims := jwt.RegisteredClaims{
		Issuer:    appleIssuer,
		Subject:   "apple-user-1",
		Audience:  jwt.ClaimStrings{"com.imgmesh.app"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verifier{Audience: "com.imgmesh.app", KeysURL: srv.URL, Client: srv.Client()}
	sub, err := v.Verify(context.Background(), signed)
	if err != nil || sub != "apple-user-1" {
		t.Fatalf("sub %q err %v", sub, err)
	}
	if _, err := v.Verify(context.Background(), signed+"x"); err == nil {
		t.Fatal("expected reject")
	}
}
