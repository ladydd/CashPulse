package store

import (
	"context"
	"time"
)

// SaveSession upserts a session id with expiry.
func (s *Store) SaveSession(ctx context.Context, id string, expiresAt time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	exp := expiresAt.UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (id, expires_at, created_at) VALUES (?, ?, ?)
ON CONFLICT(id) DO UPDATE SET expires_at = excluded.expires_at`, id, exp, now)
	return err
}

// GetSessionExpiry returns expiry if session is valid (not expired).
func (s *Store) GetSessionExpiry(ctx context.Context, id string) (time.Time, bool, error) {
	var expStr string
	err := s.db.QueryRowContext(ctx, `SELECT expires_at FROM sessions WHERE id = ?`, id).Scan(&expStr)
	if err != nil {
		return time.Time{}, false, nil // treat missing as false
	}
	exp, err := time.Parse(time.RFC3339Nano, expStr)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, expStr)
		if err != nil {
			return time.Time{}, false, err
		}
	}
	if time.Now().After(exp) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
		return time.Time{}, false, nil
	}
	return exp, true, nil
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// PurgeExpiredSessions deletes expired rows.
func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
	return err
}
