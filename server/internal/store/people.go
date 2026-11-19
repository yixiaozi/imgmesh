package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) AddComment(ctx context.Context, photoID, userID, body string) (PhotoComment, error) {
	uid, err := uuid.Parse(userID)
	if err != nil || photoID == "" || body == "" {
		return PhotoComment{}, ErrBad
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo_cards WHERE id=$1)`, photoID).Scan(&exists); err != nil {
		return PhotoComment{}, err
	}
	if !exists {
		return PhotoComment{}, ErrNotFound
	}
	comment := PhotoComment{ID: uuid.NewString(), PhotoID: photoID, UserID: userID, Body: body}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO photo_comments (id, photo_id, user_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at
	`, comment.ID, photoID, uid, body).Scan(&comment.CreatedAt)
	if err != nil {
		return PhotoComment{}, err
	}
	profile, err := s.Profile(ctx, userID)
	if err != nil {
		return PhotoComment{}, err
	}
	comment.DisplayName = profile.DisplayName
	return comment, nil
}

func (s *Store) DeleteComment(ctx context.Context, photoID, commentID, userID string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return ErrBad
	}
	cid, err := uuid.Parse(commentID)
	if err != nil {
		return ErrBad
	}
	var owner string
	err = s.pool.QueryRow(ctx, `SELECT user_id::text FROM photo_comments WHERE id=$1 AND photo_id=$2`, cid, photoID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != userID {
		return ErrConflict
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM photo_comments WHERE id=$1 AND user_id=$2`, cid, uid)
	return err
}

func (s *Store) Comments(ctx context.Context, photoID string) ([]PhotoComment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.photo_id, u.id::text, u.display_name, c.body, c.created_at
		FROM photo_comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.photo_id=$1
		ORDER BY c.created_at
		LIMIT 50
	`, photoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PhotoComment{}
	for rows.Next() {
		var comment PhotoComment
		if err := rows.Scan(&comment.ID, &comment.PhotoID, &comment.UserID, &comment.DisplayName, &comment.Body, &comment.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, comment)
	}
	return out, rows.Err()
}

type FeedItem struct {
	ID      string
	Name    string
	Kind    string
	Summary string
	At      time.Time
	Read    bool
}

func (s *Store) Feed(ctx context.Context, userID string) ([]FeedItem, int, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, 0, ErrBad
	}
	rows, err := s.pool.Query(ctx, `
		SELECT feed.id, feed.name, feed.kind, feed.summary, feed.at, r.read_at
		FROM (
			SELECT 'event:' || e.id::text AS id,
			       COALESCE(NULLIF(e.display_name, ''), '有人') AS name,
			       e.kind,
			       '' AS summary,
			       e.created_at AS at
			FROM events e
			JOIN shares s ON s.id = e.share_id
			WHERE s.owner_id=$1 AND e.user_id IS NOT NULL AND e.user_id <> $1
			  AND e.kind IN ('view', 'open', 'download', 'save', 'append')
			UNION ALL
			SELECT 'like:' || l.photo_id || ':' || l.user_id::text,
			       u.display_name,
			       'like',
			       '',
			       l.created_at
			FROM photo_likes l
			JOIN users u ON u.id = l.user_id
			WHERE l.user_id <> $1
			  AND EXISTS (
			    SELECT 1 FROM photo_people p
			    WHERE p.photo_id = l.photo_id AND p.user_id = $1 AND p.shared
			  )
			UNION ALL
			SELECT 'friend:' || other.id::text || ':' || ((extract(epoch FROM f.created_at) * 1000)::bigint)::text,
			       other.display_name,
			       'friend',
			       '',
			       f.created_at
			FROM friendships f
			JOIN users other ON other.id = f.requester_id
			WHERE f.status = 'pending' AND f.requester_id <> $1
			  AND (f.user_low = $1 OR f.user_high = $1)
			UNION ALL
			SELECT 'message:' || m.id::text,
			       u.display_name,
			       'message',
			       m.body,
			       m.created_at
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.recipient_id = $1
			  AND EXISTS (
			    SELECT 1 FROM friendships f
			    WHERE f.status = 'accepted'
			      AND ((f.user_low = m.sender_id AND f.user_high = m.recipient_id)
			        OR (f.user_high = m.sender_id AND f.user_low = m.recipient_id))
			  )
		) feed
		LEFT JOIN activity_reads r ON r.user_id = $1 AND r.activity_id = feed.id
		ORDER BY feed.at DESC
		LIMIT 40
	`, uid)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []FeedItem{}
	unread := 0
	for rows.Next() {
		var item FeedItem
		var readAt *time.Time
		if err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.Summary, &item.At, &readAt); err != nil {
			return nil, 0, err
		}
		item.Read = readAt != nil
		if !item.Read {
			unread++
		}
		items = append(items, item)
	}
	return items, unread, rows.Err()
}

func (s *Store) MarkFeed(ctx context.Context, userID, activityID string) error {
	uid, err := uuid.Parse(userID)
	if err != nil || activityID == "" || len(activityID) > 200 {
		return ErrBad
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO activity_reads (user_id, activity_id) VALUES ($1, $2)
		ON CONFLICT (user_id, activity_id) DO UPDATE SET read_at = now()
	`, uid, activityID)
	return err
}

func (s *Store) SetRecovery(ctx context.Context, userID string, hash []byte) error {
	uid, err := uuid.Parse(userID)
	if err != nil || len(hash) != 32 {
		return ErrBad
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET recovery_hash=$2 WHERE id=$1`, uid, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UserByRecovery(ctx context.Context, hash []byte) (string, error) {
	if len(hash) != 32 {
		return "", ErrBadCode
	}
	var userID string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE recovery_hash=$1`, hash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrBadCode
	}
	return userID, err
}

type DeviceInfo struct {
	ID        string
	Platform  string
	CreatedAt time.Time
}

func (s *Store) AttachDevice(ctx context.Context, deviceID, userID, platform string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return ErrBad
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO devices (id, user_id, platform) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform
	`, deviceID, uid, platform)
	return err
}

func (s *Store) Devices(ctx context.Context, userID string) ([]DeviceInfo, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrBad
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, platform, created_at FROM devices WHERE user_id=$1 ORDER BY created_at
	`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceInfo{}
	for rows.Next() {
		var device DeviceInfo
		if err := rows.Scan(&device.ID, &device.Platform, &device.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, device)
	}
	return out, rows.Err()
}
