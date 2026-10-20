package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"nhi-backend-api/internal/database"
)

const globalRefreshTokensTable = "refresh_tokens"

func (r *UserRepository) refreshTable() string {
	return database.Table(r.schema, globalRefreshTokensTable)
}

// CreateRefreshToken stores the hash of a newly issued refresh token under the
// given session (the chain of tokens created from one login).
func (r *UserRepository) CreateRefreshToken(ctx context.Context, userID int, sessionID, tokenHash string, expiresAt time.Time) error {
	db, err := r.globalDB()
	if err != nil {
		return err
	}
	query := `INSERT INTO ` + r.refreshTable() + ` (user_id, session_id, token_hash, expires_at) VALUES ($1, $2::uuid, $3, $4)`
	if _, err := db.ExecContext(ctx, query, userID, sessionID, tokenHash, expiresAt.UTC()); err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	return nil
}

// ConsumeRefreshToken atomically revokes a valid (unrevoked, unexpired) token
// of a live session and returns its owner, session id and when the session
// began (its first token). found=false means the token is unknown, expired or
// already used. reused=true means it existed but was already revoked, so the
// caller should treat it as theft and revoke the user's other sessions.
func (r *UserRepository) ConsumeRefreshToken(ctx context.Context, tokenHash string) (userID int, sessionID string, startedAt time.Time, found, reused bool, err error) {
	db, err := r.globalDB()
	if err != nil {
		return 0, "", time.Time{}, false, false, err
	}
	query := `UPDATE ` + r.refreshTable() + ` t SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL AND session_ended_at IS NULL AND expires_at > NOW()
		RETURNING user_id, session_id::text,
			(SELECT MIN(s.created_at) FROM ` + r.refreshTable() + ` s WHERE s.session_id = t.session_id)`
	err = db.QueryRowContext(ctx, query, tokenHash).Scan(&userID, &sessionID, &startedAt)
	if err == nil {
		return userID, sessionID, startedAt, true, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", time.Time{}, false, false, fmt.Errorf("consume refresh token: %w", err)
	}
	// Not consumable: distinguish "already revoked" (reuse) from unknown/expired.
	var owner int
	var revoked bool
	// A token from a session that was logged out is simply invalid: an old tab
	// refreshing after sign-out must not end the user's other sessions.
	lookup := `SELECT user_id, revoked_at IS NOT NULL AND session_ended_at IS NULL FROM ` + r.refreshTable() + ` WHERE token_hash = $1`
	err = db.QueryRowContext(ctx, lookup, tokenHash).Scan(&owner, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", time.Time{}, false, false, nil
	}
	if err != nil {
		return 0, "", time.Time{}, false, false, fmt.Errorf("lookup refresh token: %w", err)
	}
	return owner, "", time.Time{}, false, revoked, nil
}

// RevokeRefreshSession ends the whole session that the given refresh token
// belongs to: every token in the chain is revoked and stamped session_ended_at,
// so the session's access tokens stop working immediately. Idempotent; an
// unknown token is ignored.
func (r *UserRepository) RevokeRefreshSession(ctx context.Context, tokenHash string) error {
	db, err := r.globalDB()
	if err != nil {
		return err
	}
	query := `UPDATE ` + r.refreshTable() + ` SET revoked_at = COALESCE(revoked_at, NOW()), session_ended_at = NOW()
		WHERE session_ended_at IS NULL
		  AND session_id = (SELECT session_id FROM ` + r.refreshTable() + ` WHERE token_hash = $1)`
	if _, err := db.ExecContext(ctx, query, tokenHash); err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}
	return nil
}

// RevokeAllRefreshTokens ends every session of a user.
func (r *UserRepository) RevokeAllRefreshTokens(ctx context.Context, userID int) error {
	db, err := r.globalDB()
	if err != nil {
		return err
	}
	query := `UPDATE ` + r.refreshTable() + ` SET revoked_at = COALESCE(revoked_at, NOW()), session_ended_at = NOW()
		WHERE user_id = $1 AND session_ended_at IS NULL`
	if _, err := db.ExecContext(ctx, query, userID); err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return nil
}
