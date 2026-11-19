package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yixiaozi/imgmesh/server/internal/id"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

const (
	MaxPhotoBytes  int64 = 50 << 20
	MaxSharePhotos       = 200
	MaxShareBytes  int64 = 2 << 30
	DefaultQuota   int64 = 5 << 30
	MaxThumbBytes  int64 = 2 << 20
	MaxManifest    int64 = 1 << 20
	pendingWindow        = 24 * time.Hour
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrQuota    = errors.New("quota")
	ErrLimit    = errors.New("limit")
	ErrGone     = errors.New("gone")
	ErrBad      = errors.New("bad request")
)

type Store struct {
	pool  *pgxpool.Pool
	Quota int64
}

type Share struct {
	ID               []byte
	OwnerID          string
	AccessTag        []byte
	ManifestObjectID []byte
	ManifestNonce    []byte
	PhotoCount       int
	TotalBytes       int64
	ExpiresAt        *time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
}

type Object struct {
	ID             []byte
	ShareID        []byte
	StorageKey     string
	ByteSize       *int64
	PlaintextBytes int64
	Kind           string
	Status         string
	UploadID       string
	Ready          bool
}

type NewObject struct {
	ID         []byte
	Kind       string
	Plaintext  int64
	Ciphertext int64
}

type Event struct {
	Kind        string
	Client      string
	DisplayName string
	DeviceID    string
	CreatedAt   time.Time
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool, Quota: DefaultQuota}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Reset(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE events, objects, shares, sessions, users CASCADE`)
	return err
}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		for _, stmt := range splitSQL(string(body)) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				tx.Rollback(ctx)
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func splitSQL(sql string) []string {
	parts := strings.Split(sql, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Store) UpsertAppleUser(ctx context.Context, sub, name string) (string, string, error) {
	var id, display string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (apple_sub, display_name)
		VALUES ($1, $2)
		ON CONFLICT (apple_sub) DO UPDATE
		SET display_name = CASE
			WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name
			ELSE users.display_name
		END
		RETURNING id::text, display_name
	`, sub, name).Scan(&id, &display)
	return id, display, err
}

func (s *Store) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	raw, err := id.Random(32)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	uid, err := uuid.Parse(userID)
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, sum[:], uid, time.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	return id.Encode(raw), nil
}

func (s *Store) UserByToken(ctx context.Context, token string) (string, string, error) {
	raw, err := id.DecodeFixed(token, 32)
	if err != nil {
		return "", "", ErrNotFound
	}
	sum := sha256.Sum256(raw)
	var userID, name string
	var exp time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT u.id::text, u.display_name, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
	`, sum[:]).Scan(&userID, &name, &exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if !exp.After(time.Now()) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, sum[:])
		return "", "", ErrNotFound
	}
	return userID, name, nil
}

func (s *Store) CreateShare(ctx context.Context, ownerID string, shareID, accessTag []byte, expires *time.Time) error {
	uid, err := uuid.Parse(ownerID)
	if err != nil {
		return ErrBad
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO shares (id, owner_id, access_tag, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING
	`, shareID, uid, accessTag, expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var existingOwner string
	var existingTag []byte
	err = s.pool.QueryRow(ctx, `SELECT owner_id::text, access_tag FROM shares WHERE id=$1`, shareID).Scan(&existingOwner, &existingTag)
	if err != nil {
		return err
	}
	if existingOwner == ownerID && hmacEqual(existingTag, accessTag) {
		return nil
	}
	return ErrConflict
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func (s *Store) Share(ctx context.Context, shareID []byte) (Share, error) {
	var sh Share
	err := s.pool.QueryRow(ctx, `
		SELECT id, owner_id::text, access_tag, manifest_object_id, manifest_nonce,
		       photo_count, total_bytes, expires_at, revoked_at, created_at
		FROM shares WHERE id=$1
	`, shareID).Scan(&sh.ID, &sh.OwnerID, &sh.AccessTag, &sh.ManifestObjectID, &sh.ManifestNonce,
		&sh.PhotoCount, &sh.TotalBytes, &sh.ExpiresAt, &sh.RevokedAt, &sh.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Share{}, ErrNotFound
	}
	return sh, err
}

func (sh Share) Unavailable() error {
	if sh.RevokedAt != nil {
		return fmt.Errorf("%w: revoked", ErrGone)
	}
	if sh.ExpiresAt != nil && !sh.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("%w: expired", ErrGone)
	}
	return nil
}

func (s *Store) Reserve(ctx context.Context, ownerID string, shareID []byte, objs []NewObject) ([]Object, []string, error) {
	uid, err := uuid.Parse(ownerID)
	if err != nil {
		return nil, nil, ErrBad
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, uid); err != nil {
		return nil, nil, err
	}
	sh, err := scanShare(ctx, tx, shareID)
	if err != nil {
		return nil, nil, err
	}
	if sh.OwnerID != ownerID {
		return nil, nil, ErrNotFound
	}
	if err := sh.Unavailable(); err != nil {
		return nil, nil, err
	}

	cutoff := time.Now().Add(-pendingWindow)
	staleRows, err := tx.Query(ctx, `SELECT storage_key FROM objects WHERE status='pending' AND created_at < $1`, cutoff)
	if err != nil {
		return nil, nil, err
	}
	var staleKeys []string
	for staleRows.Next() {
		var k string
		if err := staleRows.Scan(&k); err != nil {
			staleRows.Close()
			return nil, nil, err
		}
		staleKeys = append(staleKeys, k)
	}
	staleRows.Close()
	if _, err := tx.Exec(ctx, `DELETE FROM objects WHERE status='pending' AND created_at < $1`, cutoff); err != nil {
		return nil, nil, err
	}

	type existing struct {
		share  []byte
		kind   string
		plain  int64
		status string
		size   *int64
	}
	known := map[string]existing{}
	for _, o := range objs {
		if err := validateNew(o); err != nil {
			return nil, nil, err
		}
		var ex existing
		err := tx.QueryRow(ctx, `SELECT share_id, kind, plaintext_bytes, status, byte_size FROM objects WHERE id=$1`, o.ID).
			Scan(&ex.share, &ex.kind, &ex.plain, &ex.status, &ex.size)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if string(ex.share) != string(shareID) {
			return nil, nil, ErrConflict
		}
		known[string(o.ID)] = ex
	}

	var photos int
	var shareBytes int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(plaintext_bytes), 0)
		FROM objects
		WHERE share_id=$1 AND kind='original' AND status IN ('pending', 'ready')
	`, shareID).Scan(&photos, &shareBytes); err != nil {
		return nil, nil, err
	}
	var used int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(o.plaintext_bytes), 0)
		FROM objects o JOIN shares s ON s.id = o.share_id
		WHERE s.owner_id=$1 AND s.revoked_at IS NULL AND o.kind='original'
		  AND (o.status='ready' OR (o.status='pending' AND o.created_at >= $2))
	`, uid, cutoff).Scan(&used); err != nil {
		return nil, nil, err
	}

	for _, o := range objs {
		ex, ok := known[string(o.ID)]
		if ok && ex.kind == "original" {
			photos--
			shareBytes -= ex.plain
			used -= ex.plain
		}
		if o.Kind == "original" {
			if ok && ex.status == "ready" {
				photos++
				shareBytes += ex.plain
				used += ex.plain
				continue
			}
			photos++
			shareBytes += o.Plaintext
			used += o.Plaintext
		}
	}
	if photos > MaxSharePhotos {
		return nil, nil, fmt.Errorf("%w: too many photos", ErrLimit)
	}
	if shareBytes > MaxShareBytes {
		return nil, nil, fmt.Errorf("%w: share too large", ErrLimit)
	}
	quota := s.Quota
	if quota <= 0 {
		quota = DefaultQuota
	}
	if used > quota {
		return nil, nil, ErrQuota
	}

	out := make([]Object, 0, len(objs))
	for _, o := range objs {
		key := id.Encode(shareID) + "/" + id.Encode(o.ID)
		ex, ok := known[string(o.ID)]
		if ok && ex.status == "ready" {
			if ex.kind != o.Kind || ex.size == nil || *ex.size != o.Ciphertext {
				return nil, nil, ErrConflict
			}
			out = append(out, Object{ID: o.ID, ShareID: shareID, StorageKey: key, ByteSize: ex.size, PlaintextBytes: o.Plaintext, Kind: o.Kind, Status: "ready", Ready: true})
			continue
		}
		if ok {
			if _, err := tx.Exec(ctx, `UPDATE objects SET plaintext_bytes=$2, kind=$3, upload_id=NULL WHERE id=$1`, o.ID, o.Plaintext, o.Kind); err != nil {
				return nil, nil, err
			}
		} else if _, err := tx.Exec(ctx, `
			INSERT INTO objects (id, share_id, storage_key, plaintext_bytes, kind, status)
			VALUES ($1, $2, $3, $4, $5, 'pending')
		`, o.ID, shareID, key, o.Plaintext, o.Kind); err != nil {
			return nil, nil, err
		}
		out = append(out, Object{ID: o.ID, ShareID: shareID, StorageKey: key, PlaintextBytes: o.Plaintext, Kind: o.Kind, Status: "pending"})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return out, staleKeys, nil
}

func validateNew(o NewObject) error {
	if len(o.ID) != 16 {
		return ErrBad
	}
	switch o.Kind {
	case "original":
		if o.Plaintext <= 0 || o.Plaintext > MaxPhotoBytes {
			return fmt.Errorf("%w: photo size", ErrLimit)
		}
	case "thumb":
		if o.Plaintext <= 0 || o.Plaintext > MaxThumbBytes {
			return fmt.Errorf("%w: thumb size", ErrLimit)
		}
	case "manifest":
		if o.Plaintext <= 0 || o.Plaintext > MaxManifest {
			return fmt.Errorf("%w: manifest size", ErrLimit)
		}
	default:
		return ErrBad
	}
	if o.Ciphertext != o.Plaintext+16 {
		return ErrBad
	}
	return nil
}

func (s *Store) SetUploadID(ctx context.Context, shareID, objectID []byte, uploadID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE objects SET upload_id=$3 WHERE id=$1 AND share_id=$2 AND status='pending'`, objectID, shareID, uploadID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Object(ctx context.Context, shareID, objectID []byte) (Object, error) {
	var o Object
	var byteSize *int64
	var uploadID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, share_id, storage_key, byte_size, plaintext_bytes, kind, status, upload_id
		FROM objects WHERE id=$1 AND share_id=$2
	`, objectID, shareID).Scan(&o.ID, &o.ShareID, &o.StorageKey, &byteSize, &o.PlaintextBytes, &o.Kind, &o.Status, &uploadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, err
	}
	o.ByteSize = byteSize
	if uploadID != nil {
		o.UploadID = *uploadID
	}
	o.Ready = o.Status == "ready"
	return o, nil
}

func (s *Store) MarkReady(ctx context.Context, shareID, objectID []byte, size int64) error {
	var status string
	var want int64
	var current *int64
	err := s.pool.QueryRow(ctx, `SELECT status, plaintext_bytes, byte_size FROM objects WHERE id=$1 AND share_id=$2`, objectID, shareID).Scan(&status, &want, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if size != want+16 {
		return ErrBad
	}
	if status == "ready" {
		if current != nil && *current == size {
			return nil
		}
		return ErrConflict
	}
	_, err = s.pool.Exec(ctx, `UPDATE objects SET status='ready', byte_size=$3 WHERE id=$1 AND share_id=$2`, objectID, shareID, size)
	return err
}

func (s *Store) Finish(ctx context.Context, ownerID string, shareID, manifestID, nonce []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	sh, err := scanShare(ctx, tx, shareID)
	if err != nil {
		return err
	}
	if sh.OwnerID != ownerID {
		return ErrNotFound
	}
	if err := sh.Unavailable(); err != nil {
		return err
	}
	var kind, status string
	err = tx.QueryRow(ctx, `SELECT kind, status FROM objects WHERE id=$1 AND share_id=$2`, manifestID, shareID).Scan(&kind, &status)
	if errors.Is(err, pgx.ErrNoRows) || kind != "manifest" || status != "ready" {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return ErrBad
	}
	var photos int
	var total int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*), COALESCE(sum(plaintext_bytes), 0)
		FROM objects WHERE share_id=$1 AND kind='original' AND status='ready'
	`, shareID).Scan(&photos, &total); err != nil {
		return err
	}
	if photos < 1 {
		return ErrBad
	}
	_, err = tx.Exec(ctx, `
		UPDATE shares
		SET manifest_object_id=$2, manifest_nonce=$3, photo_count=$4, total_bytes=$5
		WHERE id=$1
	`, shareID, manifestID, nonce, photos, total)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListMine(ctx context.Context, ownerID string) ([]Share, error) {
	uid, err := uuid.Parse(ownerID)
	if err != nil {
		return nil, ErrBad
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id::text, access_tag, manifest_object_id, manifest_nonce,
		       photo_count, total_bytes, expires_at, revoked_at, created_at
		FROM shares WHERE owner_id=$1
		ORDER BY created_at DESC
	`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		sh, err := scanShareRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *Store) Revoke(ctx context.Context, ownerID string, shareID []byte) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	sh, err := scanShare(ctx, tx, shareID)
	if err != nil {
		return nil, err
	}
	if sh.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	rows, err := tx.Query(ctx, `SELECT storage_key FROM objects WHERE share_id=$1`, shareID)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if _, err := tx.Exec(ctx, `DELETE FROM objects WHERE share_id=$1`, shareID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE shares
		SET revoked_at=now(), photo_count=0, total_bytes=0, manifest_object_id=NULL, manifest_nonce=NULL
		WHERE id=$1
	`, shareID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *Store) AddEvent(ctx context.Context, shareID []byte, kind, client, name, device string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (share_id, kind, client, display_name, device_id)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''))
	`, shareID, kind, client, name, device)
	return err
}

func (s *Store) Events(ctx context.Context, shareID []byte) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, client, COALESCE(display_name, ''), COALESCE(device_id, ''), created_at
		FROM events WHERE share_id=$1 ORDER BY created_at
	`, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Kind, &e.Client, &e.DisplayName, &e.DeviceID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type rowQuery interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanShare(ctx context.Context, q rowQuery, shareID []byte) (Share, error) {
	var sh Share
	err := q.QueryRow(ctx, `
		SELECT id, owner_id::text, access_tag, manifest_object_id, manifest_nonce,
		       photo_count, total_bytes, expires_at, revoked_at, created_at
		FROM shares WHERE id=$1
	`, shareID).Scan(&sh.ID, &sh.OwnerID, &sh.AccessTag, &sh.ManifestObjectID, &sh.ManifestNonce,
		&sh.PhotoCount, &sh.TotalBytes, &sh.ExpiresAt, &sh.RevokedAt, &sh.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Share{}, ErrNotFound
	}
	return sh, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanShareRow(rows scannable) (Share, error) {
	var sh Share
	err := rows.Scan(&sh.ID, &sh.OwnerID, &sh.AccessTag, &sh.ManifestObjectID, &sh.ManifestNonce,
		&sh.PhotoCount, &sh.TotalBytes, &sh.ExpiresAt, &sh.RevokedAt, &sh.CreatedAt)
	return sh, err
}
