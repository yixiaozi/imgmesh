package store

import (
	"context"
	"time"

	"github.com/yixiaozi/imgmesh/server/internal/id"
)

type AdminUser struct {
	Name      string
	City      string
	Platform  string
	DeviceID  string
	CreatedAt time.Time
}

type AdminShare struct {
	ID        string
	Owner     string
	Photos    int
	Bytes     int64
	Stored    int64
	Revoked   bool
	CreatedAt time.Time
}

type AdminPhoto struct {
	Owner  string
	Bytes  int64
	Stored int64
	Status string
	At     time.Time
}

type AdminEvent struct {
	At     time.Time
	Kind   string
	Client string
	Name   string
}

type AdminOverview struct {
	Users             int
	Devices           int
	Shares            int
	PhotosTransferred int64
	StorageBytes      int64
	People            []AdminUser
	ShareList         []AdminShare
	Photos            []AdminPhoto
	Events            []AdminEvent
}

func (s *Store) AdminOverview(ctx context.Context) (AdminOverview, error) {
	var out AdminOverview
	out.People = []AdminUser{}
	out.ShareList = []AdminShare{}
	out.Photos = []AdminPhoto{}
	out.Events = []AdminEvent{}
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM devices),
			(SELECT count(*) FROM shares),
			(SELECT photos_transferred FROM usage_totals),
			(SELECT COALESCE(sum(byte_size), 0)::bigint FROM objects WHERE status = 'ready')
	`).Scan(&out.Users, &out.Devices, &out.Shares, &out.PhotosTransferred, &out.StorageBytes)
	if err != nil {
		return out, err
	}
	people, err := s.pool.Query(ctx, `
		SELECT u.display_name, u.city, COALESCE(d.platform, ''), COALESCE(d.id, ''), u.created_at
		FROM users u
		LEFT JOIN devices d ON d.user_id = u.id
		ORDER BY u.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return out, err
	}
	defer people.Close()
	for people.Next() {
		var row AdminUser
		if err := people.Scan(&row.Name, &row.City, &row.Platform, &row.DeviceID, &row.CreatedAt); err != nil {
			return out, err
		}
		out.People = append(out.People, row)
	}
	if err := people.Err(); err != nil {
		return out, err
	}

	shares, err := s.pool.Query(ctx, `
		SELECT s.id, COALESCE(u.display_name, ''), s.photo_count, s.total_bytes,
			COALESCE((SELECT sum(o.byte_size) FROM objects o WHERE o.share_id = s.id AND o.status = 'ready'), 0)::bigint,
			s.revoked_at IS NOT NULL, s.created_at
		FROM shares s
		LEFT JOIN users u ON u.id = s.owner_id
		ORDER BY s.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return out, err
	}
	defer shares.Close()
	for shares.Next() {
		var raw []byte
		var row AdminShare
		if err := shares.Scan(&raw, &row.Owner, &row.Photos, &row.Bytes, &row.Stored, &row.Revoked, &row.CreatedAt); err != nil {
			return out, err
		}
		row.ID = id.Encode(raw)
		out.ShareList = append(out.ShareList, row)
	}
	if err := shares.Err(); err != nil {
		return out, err
	}

	photos, err := s.pool.Query(ctx, `
		SELECT COALESCE(u.display_name, ''), o.plaintext_bytes, COALESCE(o.byte_size, 0), o.status, o.created_at
		FROM objects o
		JOIN shares s ON s.id = o.share_id
		LEFT JOIN users u ON u.id = s.owner_id
		WHERE o.kind = 'original'
		ORDER BY o.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return out, err
	}
	defer photos.Close()
	for photos.Next() {
		var row AdminPhoto
		if err := photos.Scan(&row.Owner, &row.Bytes, &row.Stored, &row.Status, &row.At); err != nil {
			return out, err
		}
		out.Photos = append(out.Photos, row)
	}
	if err := photos.Err(); err != nil {
		return out, err
	}

	events, err := s.pool.Query(ctx, `
		SELECT e.created_at, e.kind, e.client, COALESCE(e.display_name, '')
		FROM events e
		ORDER BY e.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return out, err
	}
	defer events.Close()
	for events.Next() {
		var row AdminEvent
		if err := events.Scan(&row.At, &row.Kind, &row.Client, &row.Name); err != nil {
			return out, err
		}
		out.Events = append(out.Events, row)
	}
	return out, events.Err()
}
