package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yixiaozi/imgmesh/server/internal/blob"
	"github.com/yixiaozi/imgmesh/server/internal/id"
	"github.com/yixiaozi/imgmesh/server/internal/store"
)

const (
	putTTL = 15 * time.Minute
	getTTL = 5 * time.Minute
)

type Verifier interface {
	Verify(ctx context.Context, identityToken string) (subject string, err error)
}

type Server struct {
	Store              *store.Store
	Disk               *blob.Disk
	S3                 *blob.S3
	Secret             []byte
	PublicBaseURL      string
	Apple              Verifier
	TeamID             string
	MultipartThreshold int64
	PartSize           int64
	DiskFreeMin        uint64
	AllowedOrigins     []string
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxShare
)

func (s *Server) Handler() http.Handler {
	if s.MultipartThreshold == 0 {
		s.MultipartThreshold = 16 << 20
	}
	if s.PartSize == 0 {
		s.PartSize = 8 << 20
	}
	if s.DiskFreeMin == 0 {
		s.DiskFreeMin = 5 << 30
	}
	r := chi.NewRouter()
	r.Use(s.cors, s.recoverer, s.log)
	r.Get("/.well-known/apple-app-site-association", s.aasa)
	r.Get("/apple-app-site-association", s.aasa)
	r.Route("/v1", func(r chi.Router) {
		r.Get("/health", s.health)
		r.Post("/auth/apple", s.authApple)
		r.Post("/register", s.register)
		r.Post("/devices/session", s.deviceSession)
		r.Put("/direct/{token}", s.directPut)
		r.Get("/direct/{token}", s.directGet)
		r.Group(func(r chi.Router) {
			r.Use(s.withUser)
			r.Get("/me", s.me)
			r.Patch("/me", s.patchMe)
			r.Get("/users", s.searchUsers)
			r.Get("/users/{id}", s.publicUser)
			r.Get("/photos", s.photoCards)
			r.Post("/shares", s.createShare)
			r.Get("/me/shares", s.listMine)
			r.Post("/shares/{id}/uploads", s.uploads)
			r.Post("/shares/{id}/uploads/complete", s.complete)
			r.Delete("/shares/{id}", s.revoke)
			r.Get("/shares/{id}/events", s.listEvents)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.withAccess)
			r.Get("/shares/{id}", s.getShare)
			r.Post("/shares/{id}/photos", s.claimPhotos)
			r.Get("/shares/{id}/manifest", s.manifest)
			r.Get("/shares/{id}/objects/{oid}", s.objectURL)
			r.Post("/shares/{id}/events", s.addEvent)
		})
	})
	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authApple(w http.ResponseWriter, r *http.Request) {
	if s.Apple == nil {
		writeJSON(w, http.StatusServiceUnavailable, errBody{"apple_unconfigured"})
		return
	}
	var body struct {
		IdentityToken string `json:"identityToken"`
		DisplayName   string `json:"displayName"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	sub, err := s.Apple.Verify(r.Context(), body.IdentityToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errBody{"unauthorized"})
		return
	}
	if uid, ok := s.optionalUser(r); ok {
		if err := s.Store.BindApple(r.Context(), uid, sub); err != nil {
			writeStoreErr(w, err)
			return
		}
		s.writeSession(w, r, uid, false)
		return
	}
	userID, _, err := s.Store.UpsertAppleUser(r.Context(), sub, cleanName(body.DisplayName))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.writeSession(w, r, userID, false)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID    string `json:"deviceId"`
		DisplayName string `json:"displayName"`
		Platform    string `json:"platform"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !validDevice(body.DeviceID) || (body.Platform != "ios" && body.Platform != "web") {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	name, ok := normalizeName(body.DisplayName)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	userID, _, already, err := s.Store.RegisterDevice(r.Context(), body.DeviceID, name, body.Platform)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.writeSession(w, r, userID, already)
}

func (s *Server) deviceSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID string `json:"deviceId"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !validDevice(body.DeviceID) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	userID, _, err := s.Store.DeviceUser(r.Context(), body.DeviceID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.writeSession(w, r, userID, true)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	s.writeProfile(w, r, userID(r))
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName *string `json:"displayName"`
		Bio         *string `json:"bio"`
		City        *string `json:"city"`
		Contact     *string `json:"contact"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.DisplayName != nil {
		name, ok := normalizeName(*body.DisplayName)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		if err := s.Store.Rename(r.Context(), userID(r), name); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	if body.Bio != nil || body.City != nil || body.Contact != nil {
		current, err := s.Store.Profile(r.Context(), userID(r))
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		bio, city, contact := current.Bio, current.City, current.Contact
		if body.Bio != nil {
			bio, err = normalizeDetail(*body.Bio, 80)
		}
		if err == nil && body.City != nil {
			city, err = normalizeDetail(*body.City, 24)
		}
		if err == nil && body.Contact != nil {
			contact, err = normalizeDetail(*body.Contact, 40)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		if err := s.Store.UpdateDetails(r.Context(), userID(r), bio, city, contact); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	s.writeProfile(w, r, userID(r))
}

func (s *Server) searchUsers(w http.ResponseWriter, r *http.Request) {
	q, ok := normalizeName(r.URL.Query().Get("q"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	people, err := s.Store.SearchUsers(r.Context(), likePattern(q))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(people))
	for _, person := range people {
		out = append(out, publicUserJSON(person))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) publicUser(w http.ResponseWriter, r *http.Request) {
	if _, err := uuid.Parse(chi.URLParam(r, "id")); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	person, err := s.Store.PublicUser(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUserJSON(person)})
}

func (s *Server) photoCards(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query()["id"]
	if len(raw) == 0 || len(raw) > 40 {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		canonical, err := canonicalFingerprint(item)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		ids = append(ids, canonical)
	}
	cards, err := s.Store.PhotoCards(r.Context(), ids)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(cards))
	for _, card := range cards {
		out = append(out, photoCardJSON(card))
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": out})
}

func (s *Server) claimPhotos(w http.ResponseWriter, r *http.Request) {
	sh := shareFrom(r)
	if err := sh.Unavailable(); err != nil {
		writeStoreErr(w, err)
		return
	}
	uid, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Items []struct {
			SHA256 string `json:"sha256"`
			Kind   string `json:"kind"`
		} `json:"items"`
	}
	if !readJSON(w, r, &body) || len(body.Items) == 0 || len(body.Items) > store.MaxSharePhotos {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	claims := make([]store.PhotoClaim, 0, len(body.Items))
	for _, item := range body.Items {
		if item.Kind != "shared" && item.Kind != "saved" {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		if item.Kind == "shared" && uid != sh.OwnerID {
			writeJSON(w, http.StatusForbidden, errBody{"access_denied"})
			return
		}
		fingerprint, err := canonicalFingerprint(item.SHA256)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		claims = append(claims, store.PhotoClaim{ID: fingerprint, Kind: item.Kind})
	}
	if err := s.Store.ClaimPhotos(r.Context(), sh.ID, uid, sh.OwnerID, claims); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeSession(w http.ResponseWriter, r *http.Request, userID string, already bool) {
	profile, err := s.Store.Profile(r.Context(), userID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	token, err := s.Store.CreateSession(r.Context(), userID, 30*24*time.Hour)
	if err != nil {
		log.Printf("session: %v", err)
		writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
		return
	}
	status := http.StatusOK
	if !already && r.URL.Path == "/v1/register" {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"token":             token,
		"alreadyRegistered": already,
		"user":              profileJSON(profile),
	})
}

func (s *Server) writeProfile(w http.ResponseWriter, r *http.Request, userID string) {
	profile, err := s.Store.Profile(r.Context(), userID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": profileJSON(profile)})
}

func profileJSON(p store.Profile) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"displayName": p.DisplayName,
		"bio":         p.Bio,
		"city":        p.City,
		"contact":     p.Contact,
		"appleLinked": p.AppleLinked,
		"deviceId":    p.DeviceID,
	}
}

func publicUserJSON(u store.PublicUser) map[string]any {
	return map[string]any{
		"id":          u.ID,
		"displayName": u.DisplayName,
		"bio":         u.Bio,
		"city":        u.City,
		"contact":     u.Contact,
		"sharedCount": u.SharedCount,
		"savedCount":  u.SavedCount,
	}
}

func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID        string     `json:"id"`
		AccessTag string     `json:"accessTag"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	shareID, err := id.Decode16(body.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	tag, err := id.Decode32(body.AccessTag)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if body.ExpiresAt != nil && !body.ExpiresAt.After(time.Now()) {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	profile, err := s.Store.Profile(r.Context(), userID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !profile.AppleLinked {
		writeJSON(w, http.StatusForbidden, errBody{"apple_required"})
		return
	}
	if err := s.Store.CreateShare(r.Context(), userID(r), shareID, tag, body.ExpiresAt); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": body.ID})
}

func (s *Server) listMine(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListMine(r.Context(), userID(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]any, 0, len(list))
	for _, sh := range list {
		out = append(out, shareJSON(sh))
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": out})
}

func (s *Server) uploads(w http.ResponseWriter, r *http.Request) {
	shareID, ok := s.ownedShare(w, r)
	if !ok {
		return
	}
	if s.S3 == nil {
		free, err := blob.FreeBytes(s.Disk.Root())
		if err != nil {
			log.Printf("disk free: %v", err)
			writeJSON(w, http.StatusInsufficientStorage, errBody{"unavailable"})
			return
		}
		if free < s.DiskFreeMin {
			writeJSON(w, http.StatusInsufficientStorage, errBody{"quota"})
			return
		}
	}
	var body struct {
		Objects []struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			Plaintext  int64  `json:"plaintextBytes"`
			Ciphertext int64  `json:"ciphertextBytes"`
		} `json:"objects"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Objects) == 0 || len(body.Objects) > 450 {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	news := make([]store.NewObject, 0, len(body.Objects))
	for _, o := range body.Objects {
		raw, err := id.Decode16(o.ID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		news = append(news, store.NewObject{ID: raw, Kind: o.Kind, Plaintext: o.Plaintext, Ciphertext: o.Ciphertext})
	}
	reserved, stale, err := s.Store.Reserve(r.Context(), userID(r), shareID, news)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, key := range stale {
		s.deleteKey(r.Context(), key)
	}
	uploads := make([]any, 0, len(reserved))
	for _, o := range reserved {
		if o.Ready {
			uploads = append(uploads, map[string]any{"id": id.Encode(o.ID), "kind": o.Kind, "ready": true, "parts": []any{}})
			continue
		}
		size := o.PlaintextBytes + 16
		multipart := o.Kind == "original" && o.PlaintextBytes > s.MultipartThreshold
		plan, err := s.presignPut(r.Context(), o.StorageKey, size, multipart)
		if err != nil {
			log.Printf("presign: %v", err)
			writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
			return
		}
		if plan.UploadID != "" {
			if err := s.Store.SetUploadID(r.Context(), shareID, o.ID, plan.UploadID); err != nil {
				writeStoreErr(w, err)
				return
			}
		}
		uploads = append(uploads, map[string]any{
			"id":       id.Encode(o.ID),
			"kind":     o.Kind,
			"ready":    false,
			"partSize": plan.PartSize,
			"uploadId": plan.UploadID,
			"parts":    plan.Parts,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploads": uploads})
}

type putPlan struct {
	UploadID string
	PartSize int64
	Parts    []map[string]any
}

func (s *Server) presignPut(ctx context.Context, key string, size int64, multipart bool) (putPlan, error) {
	if !multipart {
		url, err := s.signPut(ctx, key, size, 0, "")
		if err != nil {
			return putPlan{}, err
		}
		return putPlan{Parts: []map[string]any{{"partNumber": 0, "url": url}}}, nil
	}
	lengths := splitParts(size, s.PartSize)
	uploadID := "disk"
	if s.S3 != nil {
		var err error
		uploadID, err = s.S3.CreateMultipart(ctx, key)
		if err != nil {
			return putPlan{}, err
		}
	}
	parts := make([]map[string]any, 0, len(lengths))
	for i, n := range lengths {
		url, err := s.signPut(ctx, key, n, uint16(i+1), uploadID)
		if err != nil {
			return putPlan{}, err
		}
		parts = append(parts, map[string]any{"partNumber": i + 1, "url": url})
	}
	return putPlan{UploadID: uploadID, PartSize: s.PartSize, Parts: parts}, nil
}

func (s *Server) signPut(ctx context.Context, key string, size int64, part uint16, uploadID string) (string, error) {
	if s.S3 != nil {
		if part == 0 {
			return s.S3.PresignPut(ctx, key, size)
		}
		return s.S3.PresignPart(ctx, key, uploadID, int32(part), size)
	}
	token, err := blob.Sign(s.Secret, 'P', key, time.Now().Add(putTTL), size, part)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(s.PublicBaseURL, "/") + "/v1/direct/" + token, nil
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	shareID, ok := s.ownedShare(w, r)
	if !ok {
		return
	}
	var body struct {
		Objects []struct {
			ID       string `json:"id"`
			ByteSize int64  `json:"byteSize"`
			Parts    []struct {
				PartNumber int    `json:"partNumber"`
				ETag       string `json:"etag"`
			} `json:"parts"`
		} `json:"objects"`
		ManifestObjectID string `json:"manifestObjectId"`
		ManifestNonce    string `json:"manifestNonce"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	for _, o := range body.Objects {
		oid, err := id.Decode16(o.ID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		obj, err := s.Store.Object(r.Context(), shareID, oid)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		parts := make([]uploadPart, len(o.Parts))
		for i, p := range o.Parts {
			parts[i] = uploadPart{PartNumber: p.PartNumber, ETag: p.ETag}
		}
		if err := s.finishObject(r.Context(), obj, o.ByteSize, parts); err != nil {
			writeStoreErr(w, err)
			return
		}
		if err := s.Store.MarkReady(r.Context(), shareID, oid, o.ByteSize); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	manifestID, err := id.Decode16(body.ManifestObjectID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	nonce, err := id.Decode12(body.ManifestNonce)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if err := s.Store.Finish(r.Context(), userID(r), shareID, manifestID, nonce); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type uploadPart struct {
	PartNumber int
	ETag       string
}

func (s *Server) finishObject(ctx context.Context, obj store.Object, size int64, parts []uploadPart) error {
	if obj.Ready {
		return nil
	}
	if obj.UploadID == "" {
		got, err := s.objectSize(ctx, obj.StorageKey)
		if err != nil {
			return store.ErrBad
		}
		if got != size {
			return store.ErrBad
		}
		return nil
	}
	if s.S3 != nil {
		completed := make([]blob.CompletedPart, 0, len(parts))
		for _, p := range parts {
			if p.PartNumber < 1 || p.ETag == "" {
				return store.ErrBad
			}
			completed = append(completed, blob.CompletedPart{Number: int32(p.PartNumber), ETag: p.ETag})
		}
		if err := s.S3.CompleteMultipart(ctx, obj.StorageKey, obj.UploadID, completed); err != nil {
			log.Printf("s3 complete: %v", err)
			return store.ErrBad
		}
	} else {
		n := len(parts)
		if n == 0 {
			return store.ErrBad
		}
		got, err := s.Disk.Assemble(obj.StorageKey, n)
		if err != nil {
			log.Printf("assemble: %v", err)
			return store.ErrBad
		}
		if got != size {
			return store.ErrBad
		}
		return nil
	}
	got, err := s.objectSize(ctx, obj.StorageKey)
	if err != nil || got != size {
		return store.ErrBad
	}
	return nil
}

func (s *Server) objectSize(ctx context.Context, key string) (int64, error) {
	if s.S3 != nil {
		return s.S3.Head(ctx, key)
	}
	return s.Disk.Size(key)
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	shareID, ok := s.ownedShare(w, r)
	if !ok {
		return
	}
	keys, err := s.Store.Revoke(r.Context(), userID(r), shareID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, key := range keys {
		s.deleteKey(r.Context(), key)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	shareID, ok := s.ownedShare(w, r)
	if !ok {
		return
	}
	act, err := s.Store.Activity(r.Context(), shareID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	events := make([]any, 0, len(act.Events))
	for _, e := range act.Events {
		events = append(events, map[string]any{
			"kind": e.Kind, "client": e.Client, "displayName": e.DisplayName, "createdAt": e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events":     events,
		"viewed":     peopleJSON(act.Viewed),
		"downloaded": peopleJSON(act.Downloaded),
		"phones":     phonesJSON(act.Phones),
	})
}

func peopleJSON(list []store.Person) []any {
	out := make([]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"displayName": p.DisplayName, "client": p.Client, "count": p.Count, "at": p.At,
		})
	}
	return out
}

func phonesJSON(list []store.Holding) []any {
	out := make([]any, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]any{
			"displayName": p.DisplayName, "client": p.Client, "savedAt": p.SavedAt,
		})
	}
	return out
}

func (s *Server) getShare(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	sh := shareFrom(r)
	body := shareJSON(sh)
	if owner, err := s.Store.PublicUser(r.Context(), sh.OwnerID); err == nil {
		body["owner"] = publicUserJSON(owner)
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	sh := shareFrom(r)
	if err := sh.Unavailable(); err != nil {
		writeStoreErr(w, err)
		return
	}
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	if len(sh.ManifestObjectID) == 0 || len(sh.ManifestNonce) != 12 {
		writeJSON(w, http.StatusConflict, errBody{"not_ready"})
		return
	}
	obj, err := s.Store.Object(r.Context(), sh.ID, sh.ManifestObjectID)
	if err != nil || !obj.Ready {
		writeJSON(w, http.StatusConflict, errBody{"not_ready"})
		return
	}
	url, err := s.signGet(r.Context(), obj.StorageKey)
	if err != nil {
		log.Printf("presign get: %v", err)
		writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":    id.Encode(sh.ManifestObjectID),
		"nonce": id.Encode(sh.ManifestNonce),
		"url":   url,
	})
}

func (s *Server) objectURL(w http.ResponseWriter, r *http.Request) {
	sh := shareFrom(r)
	if err := sh.Unavailable(); err != nil {
		writeStoreErr(w, err)
		return
	}
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	oid, err := id.Decode16(chi.URLParam(r, "oid"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	obj, err := s.Store.Object(r.Context(), sh.ID, oid)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !obj.Ready {
		writeJSON(w, http.StatusConflict, errBody{"not_ready"})
		return
	}
	url, err := s.signGet(r.Context(), obj.StorageKey)
	if err != nil {
		log.Printf("presign get: %v", err)
		writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) addEvent(w http.ResponseWriter, r *http.Request) {
	sh := shareFrom(r)
	if err := sh.Unavailable(); err != nil {
		writeStoreErr(w, err)
		return
	}
	uid, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Kind        string `json:"kind"`
		Client      string `json:"client"`
		DisplayName string `json:"displayName"`
		DeviceID    string `json:"deviceId"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	kind := body.Kind
	if kind == "open" {
		kind = "view"
	}
	switch kind {
	case "view", "download", "save", "remove":
	default:
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	switch body.Client {
	case "web", "ios":
	default:
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	if kind == "save" && body.Client != "ios" {
		kind = "download"
	}
	if err := s.Store.AddEvent(r.Context(), sh.ID, uid, kind, body.Client); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signGet(ctx context.Context, key string) (string, error) {
	if s.S3 != nil {
		return s.S3.PresignGet(ctx, key)
	}
	token, err := blob.Sign(s.Secret, 'G', key, time.Now().Add(getTTL), 0, 0)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(s.PublicBaseURL, "/") + "/v1/direct/" + token, nil
}

func (s *Server) directPut(w http.ResponseWriter, r *http.Request) {
	if s.Disk == nil {
		http.NotFound(w, r)
		return
	}
	ticket, err := blob.Verify(s.Secret, chi.URLParam(r, "token"), time.Now())
	if err != nil || ticket.Method != 'P' || !blob.ValidKey(ticket.Key) {
		writeJSON(w, http.StatusForbidden, errBody{"access_denied"})
		return
	}
	n, etag, err := s.Disk.Write(ticket.Key, ticket.Part, r.Body, ticket.MaxBytes)
	if err != nil {
		log.Printf("blob write: %v", err)
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return
	}
	w.Header().Set("ETag", `"`+etag+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"bytes": n})
}

func (s *Server) directGet(w http.ResponseWriter, r *http.Request) {
	if s.Disk == nil {
		http.NotFound(w, r)
		return
	}
	ticket, err := blob.Verify(s.Secret, chi.URLParam(r, "token"), time.Now())
	if err != nil || ticket.Method != 'G' || ticket.Part != 0 || !blob.ValidKey(ticket.Key) {
		writeJSON(w, http.StatusForbidden, errBody{"access_denied"})
		return
	}
	f, err := s.Disk.Open(ticket.Key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "blob", st.ModTime(), f)
}

func (s *Server) aasa(w http.ResponseWriter, r *http.Request) {
	appID := "REPLACE.com.imgmesh.app"
	if s.TeamID != "" {
		appID = s.TeamID + ".com.imgmesh.app"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"applinks": map[string]any{
			"apps": []any{},
			"details": []any{map[string]any{
				"appIDs": []string{appID},
				"components": []any{
					map[string]string{"/": "/s/*", "comment": "imgmesh share links"},
				},
			}},
		},
	})
}

func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := s.optionalUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errBody{"unauthorized"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxUser, uid)))
	})
}

func (s *Server) optionalUser(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" || strings.Contains(token, " ") {
		return "", false
	}
	uid, _, err := s.Store.UserByToken(r.Context(), token)
	if err != nil {
		return "", false
	}
	return uid, true
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := s.optionalUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errBody{"unauthorized"})
		return "", false
	}
	return uid, true
}

func (s *Server) withAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shareID, err := id.Decode16(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
			return
		}
		sh, err := s.Store.Share(r.Context(), shareID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		tag, err := id.Decode32(r.Header.Get("X-Imgmesh-Access"))
		if err != nil || subtle.ConstantTimeCompare(tag, sh.AccessTag) != 1 {
			writeJSON(w, http.StatusUnauthorized, errBody{"access_denied"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxShare, sh)))
	})
}

func (s *Server) ownedShare(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	shareID, err := id.Decode16(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return nil, false
	}
	sh, err := s.Store.Share(r.Context(), shareID)
	if err != nil {
		writeStoreErr(w, err)
		return nil, false
	}
	if sh.OwnerID != userID(r) {
		writeJSON(w, http.StatusNotFound, errBody{"not_found"})
		return nil, false
	}
	return shareID, true
}

func (s *Server) deleteKey(ctx context.Context, key string) {
	var err error
	if s.S3 != nil {
		err = s.S3.Delete(ctx, key)
	} else if s.Disk != nil {
		err = s.Disk.Delete(key)
	}
	if err != nil {
		log.Printf("delete blob: %v", err)
	}
}

func userID(r *http.Request) string { return r.Context().Value(ctxUser).(string) }

func shareFrom(r *http.Request) store.Share { return r.Context().Value(ctxShare).(store.Share) }

func shareJSON(sh store.Share) map[string]any {
	expired := sh.ExpiresAt != nil && !sh.ExpiresAt.After(time.Now())
	return map[string]any{
		"id":         id.Encode(sh.ID),
		"photoCount": sh.PhotoCount,
		"totalBytes": sh.TotalBytes,
		"createdAt":  sh.CreatedAt,
		"expiresAt":  sh.ExpiresAt,
		"revoked":    sh.RevokedAt != nil,
		"expired":    expired,
	}
}

type errBody struct {
	Error string `json:"error"`
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody{"not_found"})
	case errors.Is(err, store.ErrNameTaken):
		writeJSON(w, http.StatusConflict, errBody{"name_taken"})
	case errors.Is(err, store.ErrNameRequired):
		writeJSON(w, http.StatusConflict, errBody{"name_required"})
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusConflict, errBody{"conflict"})
	case errors.Is(err, store.ErrQuota):
		writeJSON(w, http.StatusInsufficientStorage, errBody{"quota"})
	case errors.Is(err, store.ErrLimit):
		writeJSON(w, http.StatusRequestEntityTooLarge, errBody{"too_large"})
	case errors.Is(err, store.ErrGone):
		code := "gone"
		if strings.Contains(err.Error(), "expired") {
			code = "expired"
		}
		if strings.Contains(err.Error(), "revoked") {
			code = "revoked"
		}
		writeJSON(w, http.StatusGone, errBody{code})
	case errors.Is(err, store.ErrBad):
		writeJSON(w, http.StatusBadRequest, errBody{"bad_request"})
	default:
		log.Printf("store: %v", err)
		writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
	}
}

func canonicalFingerprint(s string) (string, error) {
	raw, err := id.DecodeFixed(s, 32)
	if err != nil {
		return "", err
	}
	return id.Encode(raw), nil
}

func likePattern(q string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(strings.ToLower(q)) + "%"
}

func normalizeDetail(s string, max int) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errDetail
		}
		n++
	}
	if n > max {
		return "", errDetail
	}
	return s, nil
}

var errDetail = errors.New("detail")

func photoCardJSON(card store.PhotoCard) map[string]any {
	sharedBy := []any{}
	holders := []any{}
	savers := []any{}
	for _, person := range card.People {
		holder := map[string]any{
			"id":          person.UserID,
			"displayName": person.DisplayName,
			"city":        person.City,
			"shared":      person.Shared,
			"saved":       person.Saved,
		}
		if person.FromID != "" {
			holder["from"] = map[string]any{"id": person.FromID, "displayName": person.FromName}
		}
		holders = append(holders, holder)
		if person.Shared {
			sharedBy = append(sharedBy, map[string]any{
				"id": person.UserID, "displayName": person.DisplayName, "city": person.City,
			})
		}
		if person.Saved {
			saver := map[string]any{
				"id": person.UserID, "displayName": person.DisplayName, "city": person.City,
			}
			if person.FromID != "" {
				saver["from"] = map[string]any{"id": person.FromID, "displayName": person.FromName}
			}
			savers = append(savers, saver)
		}
	}
	return map[string]any{
		"id":       card.ID,
		"sharedBy": sharedBy,
		"holders":  holders,
		"savers":   savers,
	}
}

func normalizeName(s string) (string, bool) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", false
	}
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
		n++
	}
	if n > 24 {
		return "", false
	}
	return s, true
}

func validDevice(s string) bool {
	if len(s) < 16 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		out = append(out, r)
		if len(out) == 40 {
			break
		}
	}
	return string(out)
}

func cleanDevice(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 64 {
		s = s[:64]
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return s
}

func splitParts(total, partSize int64) []int64 {
	if partSize <= 0 {
		partSize = total
	}
	var out []int64
	for total > 0 {
		n := partSize
		if n > total {
			n = total
		}
		out = append(out, n)
		total -= n
	}
	if len(out) == 0 {
		out = []int64{0}
	}
	return out
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.AllowedOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Imgmesh-Access")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		path := r.URL.Path
		if strings.Contains(path, "/v1/direct/") {
			path = "/v1/direct/:token"
		}
		log.Printf("%s %s %d %s", r.Method, path, ww.status, time.Since(start).Truncate(time.Millisecond))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeJSON(w, http.StatusInternalServerError, errBody{"internal"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
