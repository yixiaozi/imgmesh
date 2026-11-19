package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/manifest", token, id.Encode(access), nil)
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

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/objects/"+id.Encode(objectID), token, id.Encode(access), nil)
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
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/events", token, id.Encode(access), map[string]any{
		"kind": "save", "client": "ios", "displayName": "旅行的人",
	})
	if res.StatusCode != 204 {
		t.Fatalf("event %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/events", token, "", nil)
	body := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("save")) || !bytes.Contains(body, []byte("phones")) || !bytes.Contains(body, []byte("发送者")) {
		t.Fatalf("events missing: %s", body)
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

func TestRegisterOnceAndActivity(t *testing.T) {
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	dir := t.TempDir()
	disk, err := blob.NewDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	api := &Server{
		Store: st, Disk: disk, Secret: bytes.Repeat([]byte{7}, 32), PublicBaseURL: base,
		Apple: fakeApple{"good-token": "sender-sub"}, DiskFreeMin: 1,
	}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}

	res := do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": "device-one-aaaaaa", "displayName": "小林", "platform": "ios",
	})
	if res.StatusCode != 201 {
		t.Fatalf("register %d %s", res.StatusCode, readBody(res))
	}
	var first struct {
		Token string `json:"token"`
		User  struct {
			DisplayName string `json:"displayName"`
		} `json:"user"`
	}
	json.NewDecoder(res.Body).Decode(&first)
	res.Body.Close()

	res = do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": "device-one-aaaaaa", "displayName": "另一个名字", "platform": "ios",
	})
	var again struct {
		Already bool `json:"alreadyRegistered"`
		User    struct {
			DisplayName string `json:"displayName"`
		} `json:"user"`
	}
	json.NewDecoder(res.Body).Decode(&again)
	res.Body.Close()
	if res.StatusCode != 200 || !again.Already || again.User.DisplayName != "小林" {
		t.Fatalf("second register %+v", again)
	}

	res = do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": "device-two-bbbbbb", "displayName": "小林", "platform": "web",
	})
	if res.StatusCode != 409 || !bytes.Contains(readBody(res), []byte("name_taken")) {
		t.Fatal("duplicate name should be rejected")
	}

	res = do(t, client, http.MethodPatch, base+"/v1/me", first.Token, "", map[string]string{"displayName": "阿宁"})
	if res.StatusCode != 200 {
		t.Fatalf("rename %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": "device-two-bbbbbb", "displayName": "小林", "platform": "web",
	})
	if res.StatusCode != 201 {
		t.Fatalf("other device %d %s", res.StatusCode, readBody(res))
	}
	var viewer struct {
		Token string `json:"token"`
	}
	json.NewDecoder(res.Body).Decode(&viewer)
	res.Body.Close()

	sender := auth(t, client, base)
	master := bytes.Repeat([]byte{0x11}, 32)
	shareID := bytes.Repeat([]byte{0x7a}, 16)
	access, err := seal.AccessTag(master, shareID)
	if err != nil {
		t.Fatal(err)
	}
	res = do(t, client, http.MethodPost, base+"/v1/shares", sender, "", map[string]any{
		"id": id.Encode(shareID), "accessTag": id.Encode(access),
	})
	if res.StatusCode != 200 {
		t.Fatalf("create %d %s", res.StatusCode, readBody(res))
	}
	plain := []byte("hello-photo")
	objectID := bytes.Repeat([]byte{0x33}, 16)
	nonce := bytes.Repeat([]byte{0x44}, 12)
	fileKey, _ := seal.Derive(master, shareID, seal.InfoFile)
	aad, _ := seal.AAD(shareID, objectID)
	ct, err := seal.Encrypt(fileKey, nonce, aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	manifestBody := []byte(`{"v":1,"title":"t","items":[]}`)
	manifestID := bytes.Repeat([]byte{0x55}, 16)
	manifestNonce := bytes.Repeat([]byte{0x66}, 12)
	manifestKey, _ := seal.Derive(master, shareID, seal.InfoManifest)
	manifestAAD, _ := seal.AAD(shareID, manifestID)
	manifestCT, err := seal.Encrypt(manifestKey, manifestNonce, manifestAAD, manifestBody)
	if err != nil {
		t.Fatal(err)
	}
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/uploads", sender, "", map[string]any{
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
			Parts []struct {
				PartNumber int    `json:"partNumber"`
				URL        string `json:"url"`
			} `json:"parts"`
		} `json:"uploads"`
	}
	json.NewDecoder(res.Body).Decode(&planned)
	res.Body.Close()
	blobs := map[string][]byte{id.Encode(objectID): ct, id.Encode(manifestID): manifestCT}
	for _, up := range planned.Uploads {
		for _, part := range up.Parts {
			req, _ := http.NewRequest(http.MethodPut, part.URL, bytes.NewReader(blobs[up.ID]))
			putRes, err := client.Do(req)
			if err != nil || putRes.StatusCode != 200 {
				t.Fatalf("put %v", err)
			}
			putRes.Body.Close()
		}
	}
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/uploads/complete", sender, "", map[string]any{
		"objects": []any{
			map[string]any{"id": id.Encode(objectID), "byteSize": len(ct)},
			map[string]any{"id": id.Encode(manifestID), "byteSize": len(manifestCT)},
		},
		"manifestObjectId": id.Encode(manifestID),
		"manifestNonce":    id.Encode(manifestNonce),
	})
	if res.StatusCode != 204 {
		t.Fatalf("complete %d %s", res.StatusCode, readBody(res))
	}

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/manifest", "", id.Encode(access), nil)
	if res.StatusCode != 401 {
		t.Fatalf("anonymous manifest %d", res.StatusCode)
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/manifest", viewer.Token, id.Encode(access), nil)
	if res.StatusCode != 200 {
		t.Fatalf("viewer manifest %d %s", res.StatusCode, readBody(res))
	}
	res.Body.Close()
	for _, kind := range []string{"view", "download", "save"} {
		res = do(t, client, http.MethodPost, base+"/v1/shares/"+id.Encode(shareID)+"/events", viewer.Token, id.Encode(access), map[string]string{
			"kind": kind, "client": "ios", "displayName": "假名字",
		})
		if res.StatusCode != 204 {
			t.Fatalf("event %s %d %s", kind, res.StatusCode, readBody(res))
		}
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+id.Encode(shareID)+"/events", sender, "", nil)
	recorded := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(recorded, []byte("小林")) || bytes.Contains(recorded, []byte("假名字")) {
		t.Fatalf("activity should use the registered name: %s", recorded)
	}
	if !bytes.Contains(recorded, []byte(`"phones"`)) || !bytes.Contains(recorded, []byte(`"downloaded"`)) || !bytes.Contains(recorded, []byte(`"viewed"`)) {
		t.Fatalf("activity lists missing: %s", recorded)
	}
}

func TestPhotoIdentityAndPeople(t *testing.T) {
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
		Store: st, Disk: disk, Secret: bytes.Repeat([]byte{7}, 32), PublicBaseURL: base,
		Apple: fakeApple{"good-token": "sub-photo"}, MultipartThreshold: 32, PartSize: 16, DiskFreeMin: 1,
	}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}

	sender := auth(t, client, base)
	res := do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": "photocard-device01", "displayName": "阿宁", "platform": "ios",
	})
	if res.StatusCode != 201 {
		t.Fatalf("register %d %s", res.StatusCode, readBody(res))
	}
	var viewer struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	json.NewDecoder(res.Body).Decode(&viewer)
	res.Body.Close()

	res = do(t, client, http.MethodPatch, base+"/v1/me", viewer.Token, "", map[string]string{
		"bio": "喜欢把海边的照片留下来", "city": "昆明", "contact": "阿宁",
	})
	if res.StatusCode != 200 || !bytes.Contains(readBody(res), []byte("昆明")) {
		t.Fatalf("profile patch failed")
	}

	res = do(t, client, http.MethodGet, base+"/v1/users?q="+url.QueryEscape("昆明"), viewer.Token, "", nil)
	found := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(found, []byte("阿宁")) {
		t.Fatalf("search %d %s", res.StatusCode, found)
	}

	master := bytes.Repeat([]byte{0x31}, 32)
	shareID := bytes.Repeat([]byte{0x32}, 16)
	access, err := seal.AccessTag(master, shareID)
	if err != nil {
		t.Fatal(err)
	}
	shareText := id.Encode(shareID)
	accessText := id.Encode(access)
	res = do(t, client, http.MethodPost, base+"/v1/shares", sender, "", map[string]any{
		"id": shareText, "accessTag": accessText,
	})
	if res.StatusCode != 200 {
		t.Fatalf("share %d %s", res.StatusCode, readBody(res))
	}
	res.Body.Close()

	res = do(t, client, http.MethodGet, base+"/v1/shares/"+shareText, sender, accessText, nil)
	meta := readBody(res)
	if !bytes.Contains(meta, []byte("发送者")) {
		t.Fatalf("share owner missing: %s", meta)
	}

	one := fingerprint("one")
	two := fingerprint("two")
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/photos", sender, accessText, map[string]any{
		"items": []map[string]string{{"sha256": one, "kind": "shared"}, {"sha256": two, "kind": "shared"}},
	})
	if res.StatusCode != 204 {
		t.Fatalf("owner claim %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/photos", viewer.Token, accessText, map[string]any{
		"items": []map[string]string{{"sha256": one, "kind": "shared"}},
	})
	if res.StatusCode != 403 {
		t.Fatalf("non-owner share claim %d", res.StatusCode)
	}
	res.Body.Close()
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/photos", viewer.Token, accessText, map[string]any{
		"items": []map[string]string{{"sha256": one, "kind": "saved"}},
	})
	if res.StatusCode != 204 {
		t.Fatalf("save claim %d %s", res.StatusCode, readBody(res))
	}

	res = do(t, client, http.MethodGet, base+"/v1/photos?id="+one+"&id="+two, viewer.Token, "", nil)
	body := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("阿宁")) || !bytes.Contains(body, []byte("发送者")) {
		t.Fatalf("cards %d %s", res.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"savers"`)) || !bytes.Contains(body, []byte(`"sharedBy"`)) {
		t.Fatalf("card lists missing: %s", body)
	}
	res = do(t, client, http.MethodGet, base+"/v1/users/"+viewer.User.ID, sender, "", nil)
	profile := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(profile, []byte(`"savedCount":1`)) {
		t.Fatalf("public profile %d %s", res.StatusCode, profile)
	}
}

func TestAppendPhotosAndFollow(t *testing.T) {
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
		Store: st, Disk: disk, Secret: bytes.Repeat([]byte{7}, 32), PublicBaseURL: base,
		Apple: fakeApple{"good-token": "sub-append"}, MultipartThreshold: 1 << 20, PartSize: 1 << 20, DiskFreeMin: 1,
	}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}
	sender := auth(t, client, base)

	master := bytes.Repeat([]byte{0x41}, 32)
	shareID := bytes.Repeat([]byte{0x42}, 16)
	access, err := seal.AccessTag(master, shareID)
	if err != nil {
		t.Fatal(err)
	}
	shareText := id.Encode(shareID)
	accessText := id.Encode(access)
	res := do(t, client, http.MethodPost, base+"/v1/shares", sender, "", map[string]any{
		"id": shareText, "accessTag": accessText,
	})
	if res.StatusCode != 200 {
		t.Fatalf("share %d %s", res.StatusCode, readBody(res))
	}
	res.Body.Close()

	uploadOne := func(plainText, manifestText string, objectFill, manifestFill byte) {
		t.Helper()
		plain := []byte(plainText)
		objectID := bytes.Repeat([]byte{objectFill}, 16)
		nonce := bytes.Repeat([]byte{0x11}, 12)
		fileKey, _ := seal.Derive(master, shareID, seal.InfoFile)
		aad, _ := seal.AAD(shareID, objectID)
		ct, err := seal.Encrypt(fileKey, nonce, aad, plain)
		if err != nil {
			t.Fatal(err)
		}
		manifestBody := []byte(manifestText)
		manifestID := bytes.Repeat([]byte{manifestFill}, 16)
		manifestNonce := bytes.Repeat([]byte{0x22}, 12)
		manifestKey, _ := seal.Derive(master, shareID, seal.InfoManifest)
		manifestAAD, _ := seal.AAD(shareID, manifestID)
		manifestCT, err := seal.Encrypt(manifestKey, manifestNonce, manifestAAD, manifestBody)
		if err != nil {
			t.Fatal(err)
		}
		res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/uploads", sender, "", map[string]any{
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
				Parts []struct {
					URL string `json:"url"`
				} `json:"parts"`
			} `json:"uploads"`
		}
		json.NewDecoder(res.Body).Decode(&planned)
		res.Body.Close()
		blobs := map[string][]byte{id.Encode(objectID): ct, id.Encode(manifestID): manifestCT}
		objects := []any{}
		for _, up := range planned.Uploads {
			if len(up.Parts) != 1 {
				t.Fatalf("parts %d", len(up.Parts))
			}
			req, _ := http.NewRequest(http.MethodPut, up.Parts[0].URL, bytes.NewReader(blobs[up.ID]))
			putRes, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if putRes.StatusCode != 200 {
				t.Fatalf("put %d %s", putRes.StatusCode, readBody(putRes))
			}
			etag := putRes.Header.Get("ETag")
			putRes.Body.Close()
			objects = append(objects, map[string]any{
				"id": up.ID, "byteSize": len(blobs[up.ID]),
				"parts": []map[string]any{{"partNumber": 0, "etag": etag}},
			})
		}
		res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/uploads/complete", sender, "", map[string]any{
			"objects": objects, "manifestObjectId": id.Encode(manifestID), "manifestNonce": id.Encode(manifestNonce),
		})
		if res.StatusCode != 204 {
			t.Fatalf("complete %d %s", res.StatusCode, readBody(res))
		}
	}
	uploadOne("first-photo-bytes-first-photo", `{"v":1,"title":"trip","items":[{"id":"a"}]}`, 0x31, 0x32)
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+shareText, sender, accessText, nil)
	body := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte(`"photoCount":1`)) || !bytes.Contains(body, []byte(`"autoSave":false`)) {
		t.Fatalf("after first %d %s", res.StatusCode, body)
	}
	uploadOne("second-photo-bytes-second-xx", `{"v":1,"title":"trip","items":[{"id":"a"},{"id":"b"}]}`, 0x33, 0x34)
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+shareText, sender, accessText, nil)
	body = readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte(`"photoCount":2`)) {
		t.Fatalf("after append %d %s", res.StatusCode, body)
	}

	res = do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": "append-viewer-device", "displayName": "后来的人", "platform": "web",
	})
	if res.StatusCode != 201 {
		t.Fatalf("register %d %s", res.StatusCode, readBody(res))
	}
	var viewer struct {
		Token string `json:"token"`
	}
	json.NewDecoder(res.Body).Decode(&viewer)
	res.Body.Close()
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/follow", viewer.Token, accessText, nil)
	if res.StatusCode != 204 {
		t.Fatalf("follow %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+shareText, viewer.Token, accessText, nil)
	if !bytes.Contains(readBody(res), []byte(`"autoSave":true`)) {
		t.Fatal("viewer should auto-save")
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+shareText+"/events", sender, "", nil)
	recorded := readBody(res)
	if !bytes.Contains(recorded, []byte("后来的人")) || !bytes.Contains(recorded, []byte(`"followers"`)) {
		t.Fatalf("followers %s", recorded)
	}
	res = do(t, client, http.MethodDelete, base+"/v1/shares/"+shareText+"/follow", viewer.Token, accessText, nil)
	if res.StatusCode != 204 {
		t.Fatalf("unfollow %d", res.StatusCode)
	}
	res = do(t, client, http.MethodGet, base+"/v1/shares/"+shareText+"/events", sender, "", nil)
	recorded = readBody(res)
	if bytes.Contains(recorded, []byte("后来的人")) {
		t.Fatalf("follower remained: %s", recorded)
	}
}

func fingerprint(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return id.Encode(sum[:])
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

func TestLikesFriendsAndMessages(t *testing.T) {
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	api := &Server{Store: st, Secret: bytes.Repeat([]byte{9}, 32), PublicBaseURL: base, Apple: fakeApple{"good-token": "sub-social"}, DiskFreeMin: 1}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}

	sender := auth(t, client, base)
	jia := registerNamed(t, client, base, "social-friend-jia01", "甲")
	yi := registerNamed(t, client, base, "social-friend-yi0001", "乙")
	bing := registerNamed(t, client, base, "social-friend-bing01", "丙")

	master := bytes.Repeat([]byte{0x61}, 32)
	shareID := bytes.Repeat([]byte{0x62}, 16)
	access, err := seal.AccessTag(master, shareID)
	if err != nil {
		t.Fatal(err)
	}
	shareText := id.Encode(shareID)
	accessText := id.Encode(access)
	res := do(t, client, http.MethodPost, base+"/v1/shares", sender, "", map[string]string{
		"id": shareText, "accessTag": accessText,
	})
	if res.StatusCode != 200 {
		t.Fatalf("share %d %s", res.StatusCode, readBody(res))
	}
	res.Body.Close()
	photo := fingerprint("liked-photo")
	res = do(t, client, http.MethodPost, base+"/v1/shares/"+shareText+"/photos", sender, accessText, map[string]any{
		"items": []map[string]string{{"sha256": photo, "kind": "shared"}},
	})
	if res.StatusCode != 204 {
		t.Fatalf("claim %d %s", res.StatusCode, readBody(res))
	}

	res = do(t, client, http.MethodPost, base+"/v1/photos/"+photo+"/like", jia.Token, "", nil)
	if res.StatusCode != 204 {
		t.Fatalf("like %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodGet, base+"/v1/photos?id="+photo, jia.Token, "", nil)
	body := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte(`"likeCount":1`)) || !bytes.Contains(body, []byte(`"liked":true`)) || !bytes.Contains(body, []byte("甲")) {
		t.Fatalf("liked card %d %s", res.StatusCode, body)
	}
	res = do(t, client, http.MethodGet, base+"/v1/photos?id="+photo, yi.Token, "", nil)
	body = readBody(res)
	if !bytes.Contains(body, []byte(`"liked":false`)) {
		t.Fatalf("peer like flag %s", body)
	}
	res = do(t, client, http.MethodDelete, base+"/v1/photos/"+photo+"/like", jia.Token, "", nil)
	if res.StatusCode != 204 {
		t.Fatalf("unlike %d", res.StatusCode)
	}
	missing := fingerprint("missing-photo")
	res = do(t, client, http.MethodPost, base+"/v1/photos/"+missing+"/like", jia.Token, "", nil)
	if res.StatusCode != 404 {
		t.Fatalf("like missing %d %s", res.StatusCode, readBody(res))
	}

	res = do(t, client, http.MethodPost, base+"/v1/friends", jia.Token, "", map[string]string{"userId": yi.ID})
	if res.StatusCode != 200 || !bytes.Contains(readBody(res), []byte(`"pending"`)) {
		t.Fatal("request should stay pending")
	}
	res = do(t, client, http.MethodGet, base+"/v1/friends", yi.Token, "", nil)
	body = readBody(res)
	if !bytes.Contains(body, []byte("甲")) || !bytes.Contains(body, []byte(`"incoming"`)) {
		t.Fatalf("incoming %s", body)
	}
	res = do(t, client, http.MethodPost, base+"/v1/friends/"+jia.ID+"/accept", yi.Token, "", nil)
	if res.StatusCode != 204 {
		t.Fatalf("accept %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodPost, base+"/v1/messages", jia.Token, "", map[string]string{"to": yi.ID, "body": "到了吗"})
	if res.StatusCode != 201 || !bytes.Contains(readBody(res), []byte("到了吗")) {
		t.Fatal("send failed")
	}
	res = do(t, client, http.MethodGet, base+"/v1/friends", yi.Token, "", nil)
	if !bytes.Contains(readBody(res), []byte(`"unread":1`)) {
		t.Fatal("unread missing")
	}
	res = do(t, client, http.MethodGet, base+"/v1/messages?with="+jia.ID, yi.Token, "", nil)
	body = readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("到了吗")) {
		t.Fatalf("thread %d %s", res.StatusCode, body)
	}
	res = do(t, client, http.MethodGet, base+"/v1/friends", yi.Token, "", nil)
	if bytes.Contains(readBody(res), []byte(`"unread":1`)) {
		t.Fatal("reading the thread should clear the unread count")
	}
	res = do(t, client, http.MethodPost, base+"/v1/messages", bing.Token, "", map[string]string{"to": jia.ID, "body": "你好"})
	if res.StatusCode != 403 || !bytes.Contains(readBody(res), []byte("not_friends")) {
		t.Fatal("stranger should not message")
	}
	res = do(t, client, http.MethodPost, base+"/v1/friends", jia.Token, "", map[string]string{"userId": jia.ID})
	if res.StatusCode != 400 {
		t.Fatalf("self friend %d", res.StatusCode)
	}
	res = do(t, client, http.MethodPost, base+"/v1/friends", bing.Token, "", map[string]string{"userId": jia.ID})
	if res.StatusCode != 200 {
		t.Fatalf("bing request %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodPost, base+"/v1/friends/"+bing.ID+"/decline", jia.Token, "", nil)
	if res.StatusCode != 204 {
		t.Fatalf("decline %d %s", res.StatusCode, readBody(res))
	}
	res = do(t, client, http.MethodPost, base+"/v1/messages", jia.Token, "", map[string]string{"to": yi.ID, "body": "   "})
	if res.StatusCode != 400 {
		t.Fatalf("blank message %d", res.StatusCode)
	}
}

func TestAdminOverview(t *testing.T) {
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	api := &Server{Store: st, Secret: bytes.Repeat([]byte{7}, 32), PublicBaseURL: base, Apple: fakeApple{}, DiskFreeMin: 1, AdminToken: "test-admin-token"}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}
	registerNamed(t, client, base, "admin-owner-device01", "海边")

	res, err := client.Get(base + "/v1/admin/overview")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("open admin %d", res.StatusCode)
	}
	res.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, base+"/v1/admin/overview", nil)
	req.Header.Set("X-Imgmesh-Admin", "nope")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("bad admin %d", res.StatusCode)
	}
	res.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, base+"/v1/admin/overview", nil)
	req.Header.Set("X-Imgmesh-Admin", "test-admin-token")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(res)
	if res.StatusCode != 200 || !bytes.Contains(body, []byte("海边")) || bytes.Contains(body, []byte("accessTag")) {
		t.Fatalf("admin %d %s", res.StatusCode, body)
	}

	closed := &Server{Store: st, Secret: bytes.Repeat([]byte{7}, 32), PublicBaseURL: base, Apple: fakeApple{}, DiskFreeMin: 1}
	rec := httptest.NewRecorder()
	closed.adminOverview(rec, httptest.NewRequest(http.MethodGet, "/v1/admin/overview", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured %d", rec.Code)
	}
}

func TestUsageStats(t *testing.T) {
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	api := &Server{Store: st, Secret: bytes.Repeat([]byte{7}, 32), PublicBaseURL: base, Apple: fakeApple{}, DiskFreeMin: 1}
	srv := &http.Server{Handler: api.Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	client := &http.Client{Timeout: 10 * time.Second}

	owner := registerNamed(t, client, base, "usage-owner-device1", "海边")
	registerNamed(t, client, base, "usage-other-device1", "阿宁")
	shareID := bytes.Repeat([]byte{0x51}, 16)
	if err := st.CreateShare(ctx, owner.ID, shareID, bytes.Repeat([]byte{0x52}, 32), nil); err != nil {
		t.Fatal(err)
	}
	originalID := bytes.Repeat([]byte{0x53}, 16)
	thumbID := bytes.Repeat([]byte{0x54}, 16)
	if _, _, err := st.Reserve(ctx, owner.ID, shareID, []store.NewObject{
		{ID: originalID, Kind: "original", Plaintext: 8, Ciphertext: 24},
		{ID: thumbID, Kind: "thumb", Plaintext: 8, Ciphertext: 24},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(ctx, shareID, originalID, 24); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(ctx, shareID, originalID, 24); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(ctx, shareID, thumbID, 24); err != nil {
		t.Fatal(err)
	}
	if err := st.AddEvent(ctx, shareID, owner.ID, "view", "web"); err != nil {
		t.Fatal(err)
	}

	var snap struct {
		Users    int   `json:"users"`
		Devices  int   `json:"devices"`
		Photos   int64 `json:"photosTransferred"`
		Activity []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"activity"`
	}
	body := httpGet(t, client, base+"/v1/stats")
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Users != 2 || snap.Devices != 2 || snap.Photos != 1 {
		t.Fatalf("stats users=%d devices=%d photos=%d %s", snap.Users, snap.Devices, snap.Photos, body)
	}
	seen := false
	for _, event := range snap.Activity {
		if event.Kind == "view" && event.Name == "海边" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("activity %s", body)
	}
	if _, err := st.Revoke(ctx, owner.ID, shareID); err != nil {
		t.Fatal(err)
	}
	body = httpGet(t, client, base+"/v1/stats")
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Photos != 1 {
		t.Fatalf("revoke changed the transfer count: %s", body)
	}
}

type namedUser struct {
	Token string
	ID    string
}

func registerNamed(t *testing.T, client *http.Client, base, device, name string) namedUser {
	t.Helper()
	res := do(t, client, http.MethodPost, base+"/v1/register", "", "", map[string]string{
		"deviceId": device, "displayName": name, "platform": "web",
	})
	if res.StatusCode != 201 {
		t.Fatalf("register %s %d %s", name, res.StatusCode, readBody(res))
	}
	var body struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	return namedUser{Token: body.Token, ID: body.User.ID}
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
