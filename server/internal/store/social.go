package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func orderedPair(a, b string) (uuid.UUID, uuid.UUID, error) {
	left, err := uuid.Parse(a)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, ErrBad
	}
	right, err := uuid.Parse(b)
	if err != nil {
		return uuid.UUID{}, uuid.UUID{}, ErrBad
	}
	if left == right {
		return uuid.UUID{}, uuid.UUID{}, ErrBad
	}
	if bytesCompare(left, right) < 0 {
		return left, right, nil
	}
	return right, left, nil
}

func bytesCompare(a, b uuid.UUID) int {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func (s *Store) RequestFriend(ctx context.Context, fromID, toID string) (string, error) {
	low, high, err := orderedPair(fromID, toID)
	if err != nil {
		return "", err
	}
	var exists bool
	err = s.pool.QueryRow(ctx, `SELECT true FROM users WHERE id=$1`, toID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var status, requester string
	err = tx.QueryRow(ctx, `
		SELECT status, requester_id::text
		FROM friendships
		WHERE user_low=$1 AND user_high=$2
		FOR UPDATE
	`, low, high).Scan(&status, &requester)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO friendships (user_low, user_high, requester_id, status)
			VALUES ($1, $2, $3, 'pending')
		`, low, high, fromID); err != nil {
			return "", err
		}
		return "pending", tx.Commit(ctx)
	}
	if err != nil {
		return "", err
	}
	if status == "accepted" || requester == fromID {
		return status, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE friendships SET status='accepted', updated_at=now()
		WHERE user_low=$1 AND user_high=$2
	`, low, high); err != nil {
		return "", err
	}
	return "accepted", tx.Commit(ctx)
}

func (s *Store) AcceptFriend(ctx context.Context, me, other string) error {
	low, high, err := orderedPair(me, other)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, requester string
	err = tx.QueryRow(ctx, `
		SELECT status, requester_id::text
		FROM friendships
		WHERE user_low=$1 AND user_high=$2
		FOR UPDATE
	`, low, high).Scan(&status, &requester)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "accepted" {
		return tx.Commit(ctx)
	}
	if requester == me {
		return ErrNotFriends
	}
	if _, err := tx.Exec(ctx, `
		UPDATE friendships SET status='accepted', updated_at=now()
		WHERE user_low=$1 AND user_high=$2
	`, low, high); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeclineFriend(ctx context.Context, me, other string) error {
	low, high, err := orderedPair(me, other)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM friendships
		WHERE user_low=$1 AND user_high=$2 AND status='pending'
		  AND (requester_id=$3 OR requester_id=$4)
	`, low, high, me, other)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var status string
		err = s.pool.QueryRow(ctx, `SELECT status FROM friendships WHERE user_low=$1 AND user_high=$2`, low, high).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

func (s *Store) RemoveFriend(ctx context.Context, me, other string) error {
	low, high, err := orderedPair(me, other)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM friendships WHERE user_low=$1 AND user_high=$2`, low, high)
	return err
}

func (s *Store) FriendLists(ctx context.Context, me string) (SocialLists, error) {
	uid, err := uuid.Parse(me)
	if err != nil {
		return SocialLists{}, ErrBad
	}
	rows, err := s.pool.Query(ctx, `
		SELECT other.id::text, other.display_name, other.bio, other.city, other.contact,
		       (SELECT count(*) FROM photo_people p WHERE p.user_id = other.id AND p.shared),
		       (SELECT count(*) FROM photo_people p WHERE p.user_id = other.id AND p.saved),
		       f.status, f.requester_id::text,
		       COALESCE((
		           SELECT m.body FROM messages m
		           WHERE (m.sender_id = $1 AND m.recipient_id = other.id)
		              OR (m.sender_id = other.id AND m.recipient_id = $1)
		           ORDER BY m.created_at DESC LIMIT 1
		       ), ''),
		       (
		           SELECT m.created_at FROM messages m
		           WHERE (m.sender_id = $1 AND m.recipient_id = other.id)
		              OR (m.sender_id = other.id AND m.recipient_id = $1)
		           ORDER BY m.created_at DESC LIMIT 1
		       ),
		       (
		           SELECT count(*) FROM messages m
		           WHERE m.sender_id = other.id AND m.recipient_id = $1
		             AND m.created_at > COALESCE(
		                 (SELECT r.read_at FROM message_reads r WHERE r.user_id = $1 AND r.peer_id = other.id),
		                 'epoch'
		             )
		       )
		FROM friendships f
		JOIN users other ON other.id = CASE WHEN f.user_low = $1 THEN f.user_high ELSE f.user_low END
		WHERE f.user_low = $1 OR f.user_high = $1
		ORDER BY f.updated_at DESC
	`, uid)
	if err != nil {
		return SocialLists{}, err
	}
	defer rows.Close()
	lists := SocialLists{Friends: []SocialUser{}, Incoming: []SocialUser{}, Outgoing: []SocialUser{}}
	for rows.Next() {
		var person SocialUser
		var status, requester string
		var lastAt *time.Time
		if err := rows.Scan(
			&person.ID, &person.DisplayName, &person.Bio, &person.City, &person.Contact,
			&person.SharedCount, &person.SavedCount, &status, &requester,
			&person.LastBody, &lastAt, &person.Unread,
		); err != nil {
			return SocialLists{}, err
		}
		if lastAt != nil {
			person.LastAt = *lastAt
		}
		switch {
		case status == "accepted":
			lists.Friends = append(lists.Friends, person)
		case requester == me:
			lists.Outgoing = append(lists.Outgoing, person)
		default:
			lists.Incoming = append(lists.Incoming, person)
		}
	}
	return lists, rows.Err()
}

func (s *Store) acceptedFriends(ctx context.Context, a, b string) error {
	low, high, err := orderedPair(a, b)
	if err != nil {
		return err
	}
	var status string
	err = s.pool.QueryRow(ctx, `SELECT status FROM friendships WHERE user_low=$1 AND user_high=$2`, low, high).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "accepted") {
		return ErrNotFriends
	}
	return err
}

func (s *Store) SendMessage(ctx context.Context, fromID, toID, body string) (DirectMessage, error) {
	if err := s.acceptedFriends(ctx, fromID, toID); err != nil {
		return DirectMessage{}, err
	}
	from, err := uuid.Parse(fromID)
	if err != nil {
		return DirectMessage{}, ErrBad
	}
	to, err := uuid.Parse(toID)
	if err != nil {
		return DirectMessage{}, ErrBad
	}
	msg := DirectMessage{ID: uuid.NewString(), SenderID: fromID, RecipientID: toID, Body: body}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO messages (id, sender_id, recipient_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at
	`, msg.ID, from, to, body).Scan(&msg.CreatedAt)
	return msg, err
}

func (s *Store) Messages(ctx context.Context, me, peer string, before *time.Time) ([]DirectMessage, error) {
	if err := s.acceptedFriends(ctx, me, peer); err != nil {
		return nil, err
	}
	meID, err := uuid.Parse(me)
	if err != nil {
		return nil, ErrBad
	}
	peerID, err := uuid.Parse(peer)
	if err != nil {
		return nil, ErrBad
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, sender_id::text, recipient_id::text, body, created_at
		FROM messages
		WHERE ((sender_id=$1 AND recipient_id=$2) OR (sender_id=$2 AND recipient_id=$1))
		  AND ($3::timestamptz IS NULL OR created_at < $3)
		ORDER BY created_at DESC
		LIMIT 50
	`, meID, peerID, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DirectMessage{}
	for rows.Next() {
		var msg DirectMessage
		if err := rows.Scan(&msg.ID, &msg.SenderID, &msg.RecipientID, &msg.Body, &msg.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO message_reads (user_id, peer_id, read_at) VALUES ($1, $2, now())
		ON CONFLICT (user_id, peer_id) DO UPDATE SET read_at = now()
	`, meID, peerID); err != nil {
		return nil, err
	}
	return out, nil
}
