package store

import (
	"context"
	"time"
)

type UsageEvent struct {
	At     time.Time
	Kind   string
	Name   string
	Detail string
}

type UsageSnapshot struct {
	Users             int
	Devices           int
	PhotosTransferred int64
	StorageBytes      int64
	Activity          []UsageEvent
}

func (s *Store) Usage(ctx context.Context) (UsageSnapshot, error) {
	var snap UsageSnapshot
	snap.Activity = []UsageEvent{}
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM devices),
			(SELECT photos_transferred FROM usage_totals),
			(SELECT COALESCE(sum(byte_size), 0)::bigint FROM objects WHERE status = 'ready')
	`).Scan(&snap.Users, &snap.Devices, &snap.PhotosTransferred, &snap.StorageBytes)
	if err != nil {
		return snap, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT at, kind, name, detail FROM (
			(SELECT created_at AS at, 'user' AS kind, display_name AS name, '' AS detail
			 FROM users WHERE display_name <> ''
			 ORDER BY created_at DESC LIMIT 15)
			UNION ALL
			(SELECT d.created_at, 'device', u.display_name, d.platform
			 FROM devices d JOIN users u ON u.id = d.user_id
			 ORDER BY d.created_at DESC LIMIT 15)
			UNION ALL
			(SELECT o.created_at, 'photo', u.display_name, ''
			 FROM objects o
			 JOIN shares s ON s.id = o.share_id
			 JOIN users u ON u.id = s.owner_id
			 WHERE o.kind = 'original' AND o.status = 'ready'
			 ORDER BY o.created_at DESC LIMIT 15)
			UNION ALL
			(SELECT created_at, kind, COALESCE(display_name, ''), client
			 FROM events
			 ORDER BY created_at DESC LIMIT 15)
		) recent
		ORDER BY at DESC
		LIMIT 15
	`)
	if err != nil {
		return snap, err
	}
	defer rows.Close()
	for rows.Next() {
		var event UsageEvent
		if err := rows.Scan(&event.At, &event.Kind, &event.Name, &event.Detail); err != nil {
			return snap, err
		}
		if event.Name == "" {
			event.Name = "未命名"
		}
		snap.Activity = append(snap.Activity, event)
	}
	return snap, rows.Err()
}
