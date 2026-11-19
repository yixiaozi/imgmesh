package seal_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yixiaozi/imgmesh/server/internal/id"
	"github.com/yixiaozi/imgmesh/server/internal/seal"
)

func TestRoundTripAndVector(t *testing.T) {
	master := bytes.Repeat([]byte{0x11}, 32)
	shareID := bytes.Repeat([]byte{0x22}, 16)
	objectID := bytes.Repeat([]byte{0x33}, 16)
	nonce := bytes.Repeat([]byte{0x44}, 12)
	plain := []byte("imgmesh")

	fileKey, err := seal.Derive(master, shareID, seal.InfoFile)
	if err != nil {
		t.Fatal(err)
	}
	manifestKey, err := seal.Derive(master, shareID, seal.InfoManifest)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := seal.AccessTag(master, shareID)
	if err != nil {
		t.Fatal(err)
	}
	aad, err := seal.AAD(shareID, objectID)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := seal.Encrypt(fileKey, nonce, aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := seal.Decrypt(fileKey, nonce, aad, ct)
	if err != nil || string(got) != string(plain) {
		t.Fatalf("decrypt %q err %v", got, err)
	}
	if _, err := seal.Decrypt(manifestKey, nonce, aad, ct); err == nil {
		t.Fatal("manifest key must not open a file ciphertext")
	}

	vector := map[string]string{
		"masterKey":   id.Encode(master),
		"shareId":     id.Encode(shareID),
		"objectId":    id.Encode(objectID),
		"nonce":       id.Encode(nonce),
		"plaintext":   id.Encode(plain),
		"fileKey":     id.Encode(fileKey),
		"manifestKey": id.Encode(manifestKey),
		"accessTag":   id.Encode(tag),
		"ciphertext":  id.Encode(ct),
		"aad":         id.Encode(aad),
	}
	path := filepath.Join("..", "..", "..", "testdata", "vector.json")
	if os.Getenv("UPDATE_VECTOR") == "1" {
		raw, _ := json.MarshalIndent(vector, "", "  ")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vector (run with UPDATE_VECTOR=1): %v", err)
	}
	var frozen map[string]string
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	for k, want := range vector {
		if frozen[k] != want {
			t.Fatalf("vector %s changed", k)
		}
	}
}
