package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yixiaozi/imgmesh/server/internal/blob"
	"github.com/yixiaozi/imgmesh/server/internal/id"
	"github.com/yixiaozi/imgmesh/server/internal/seal"
	"github.com/yixiaozi/imgmesh/server/internal/store"
)

type fakeApple map[string]string

func (f fakeApple) Verify(ctx context.Context, token string) (string, error) {
	sub, ok := f[token]
	if !ok {
		return "", os.ErrInvalid
	}
	return sub, nil
}

func TestShareSaveAndRead(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres:///imgmesh_test?host=/var/run/postgresql"
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Reset(ctx); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	disk, err := blob.NewDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	api := &Server{
		Store:              st,
		Disk:               disk,
		Secret:             bytes.Repeat([]byte{7}, 32),
		PublicBaseURL:      base,
		Apple:              fakeApple{"good-token": "sub-1"},
		MultipartThreshold: 32,
		PartSize:           16,
		DiskFreeMin:        1,
		AllowedOrigins:     []string{"http://localhost:5173"},
	}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	client := &http.Client{Timeout: 10 * time.Second}
	token := auth(t, client, base)

	master := bytes.Repeat([]byte{0x11}, 32)
	shareID := bytes.Repeat([]byte{0x22}, 16)
	access, err := seal.AccessTag(master, shareID)
	if err != nil {
		t.Fatal(err)
	}
	res := do(t, client, http.MethodPost, base+"/v1/shares", token, "", map[string]any{
		"id": id.Encode(shareID), "accessTag": id.Encode(access),
	})
	if res.StatusCode != 200 {
		t.Fatalf("create %d %s", res.StatusCode, readBody(res))
	}

	plain := bytes.Repeat([]byte("photo-bytes-"), 8) // 96 bytes, multipart
	objectID := bytes.Repeat([]byte{0x33}, 16)
	nonce := bytes.Repeat([]byte{0x44}, 12)
	fileKey, _ := seal.Derive(master, shareID, seal.InfoFile)
	aad, _ := seal.AAD(shareID, objectID)
	ct, err := seal.Encrypt(fileKey, nonce, aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	manifestBody := []byte(`{"v":1,"title":"trip","items":[]}`)
	manifestID := bytes.Repeat([]byte{0x55}, 16)
	manifestNonce := bytes.Repeat([]byte{0x66}, 12)
	manifestKey, _ := seal.Derive(master, shareID, seal.InfoManifest)
	manifestAAD, _ := seal.AAD(shareID, manifestID)
	manifestCT, err := seal.Encrypt(manifestKey, manifestNonce, manifestAAD, manifestBody)
	if err != nil {
		t.Fatal(err)
	}

	res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/uploads", token, "", map[string]any{
		"objects": []any{
			map[string]any{"id": id.Encode(objectID), "kind": "original", "plaintextBytes": len(plain), "ciphertextBytes": len(ct)},
			map[string]any{"id": id.Encode(manifestID), "kind": "manifest", "plaintextBytes": len(manifestBody), "ciphertextBytes": len(manifestCT)},
		},
	})
	if res.StatusCode != 200 {
		t.Fatalf("uploads %d %s", res.StatusCode, readBody(res))
	}
	var planned struct {
		Uploads []struct {
			ID    string `json:"id"`
			Ready bool   `json:"ready"`
			Parts []struct {
				PartNumber int    `json:"partNumber"`
				URL        string `json:"url"`
			} `json:"parts"`
		} `json:"uploads"`
	}
	json.NewDecoder(res.Body).Decode(&planned)
	res.Body.Close()
	blobs := map[string][]byte{id.Encode(objectID): ct, id.Encode(manifestID): manifestCT}
	type donePart struct {
		PartNumber int    `json:"partNumber"`
		ETag       string `json:"etag"`
	}
	completed := map[string][]donePart{}
	for _, up := range planned.Uploads {
		payload := blobs[up.ID]
		off := 0
		for _, part := range up.Parts {
			end := len(payload)
			if part.PartNumber > 0 {
				end = off + int(api.PartSize)
				if end > len(payload) {
					end = len(payload)
				}
			}
			req, _ := http.NewRequest(http.MethodPut, part.URL, bytes.NewReader(payload[off:end]))
			putRes, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if putRes.StatusCode != 200 {
				t.Fatalf("put %d %s", putRes.StatusCode, readBody(putRes))
			}
			putRes.Body.Close()
			completed[up.ID] = append(completed[up.ID], donePart{PartNumber: part.PartNumber, ETag: putRes.Header.Get("ETag")})
			if part.PartNumber > 0 {
				off = end
			}
		}
	}
	objects := []any{}
	for _, up := range planned.Uploads {
		size := len(blobs[up.ID])
		objects = append(objects, map[string]any{"id": up.ID, "byteSize": size, "parts": completed[up.ID]})
	}
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/uploads/complete", token, "", map[string]any{
		"objects": objects, "manifestObjectId": id.Encode(manifestID), "manifestNonce": id.Encode(manifestNonce),
	})
	if res.StatusCode != 204 {
		t.Fatalf("complete %d %s", res.StatusCode, readBody(res))
	}

	// Ciphertext on disk is not the plaintext.
	stored, err := os.ReadFile(filepath.Join(dir, id.Encode(shareID), id.Encode(objectID)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, plain[:12]) {
		t.Fatal("plaintext leaked into object storage")
	}

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/manifest", "", id.Encode(access), nil)
	if res.StatusCode != 200 {
		t.Fatalf("manifest %d %s", res.StatusCode, readBody(res))
	}
	var man struct {
		Nonce string `json:"nonce"`
		URL   string `json:"url"`
	}
	json.NewDecoder(res.Body).Decode(&man)
	res.Body.Close()
	if man.Nonce != id.Encode(manifestNonce) {
		t.Fatalf("nonce %s", man.Nonce)
	}
	gotMan := httpGet(t, client, man.URL)
	opened, err := seal.Decrypt(manifestKey, manifestNonce, manifestAAD, gotMan)
	if err != nil || !bytes.Equal(opened, manifestBody) {
		t.Fatalf("manifest decrypt %v %q", err, opened)
	}

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/objects/"+id.Encode(objectID), "", id.Encode(access), nil)
	var obj struct {
		URL string `json:"url"`
	}
	json.NewDecoder(res.Body).Decode(&obj)
	res.Body.Close()
	got := httpGet(t, client, obj.URL)
	opened, err = seal.Decrypt(fileKey, nonce, aad, got)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("photo decrypt %v", err)
	}

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID), "", "", nil)
	if res.StatusCode != 401 {
		t.Fatalf("missing tag %d", res.StatusCode)
	}
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/events", "", id.Encode(access), map[string]any{
		"kind": "save", "client": "ios", "displayName": "旅行的人",
	})
	if res.StatusCode != 204 {
		t.Fatalf("event %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/events", token, "", nil)
	if res.StatusCode != 200 || !bytes.Contains(readBody(res), []byte("save")) {
		t.Fatalf("events missing")
	}

	res = do(t, client, http.MethodDelete, base+"/v1/shares/"+id.Encode(shareID), token, "", nil)
	if res.StatusCode != 204 {
		t.Fatalf("revoke %d", res.StatusCode)
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/objects/"+id.Encode(objectID), "", id.Encode(access), nil)
	if res.StatusCode != 410 {
		t.Fatalf("after revoke %d %s", res.StatusCode, readBody(res))
	}
	if _, err := os.Stat(filepath.Join(dir, id.Encode(shareID), id.Encode(objectID))); !os.IsNotExist(err) {
		t.Fatal("ciphertext should be removed on revoke")
	}
}

func auth(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	res := do(t, client, http.MethodPost, base+"/v1/auth/apple", "", "", map[string]string{
		"identityToken": "good-token", "displayName": "发送者",
	})
	if res.StatusCode != 200 {
		t.Fatalf("auth %d %s", res.StatusCode, readBody(res))
	}
	var body struct {
		Token string `json:"token"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	return body.Token
}

func do(t *testing.T, client *http.Client, method, url, bearer, access string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if access != "" {
		req.Header.Set("X-Imgmesh-Access", access)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func httpGet(t *testing.T, client *http.Client, url string) []byte {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("get %s %d %s", url, res.StatusCode, readBody(res))
	}
	b, _ := io.ReadAll(res.Body)
	return b
}

func readBody(res *http.Response) []byte {
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return b
}
