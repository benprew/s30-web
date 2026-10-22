package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	errUserExists       = errors.New("username or email already taken")
	errUserNotFound     = errors.New("user not found")
	errSaveNotFound     = errors.New("save not found")
	errGameNotFound     = errors.New("game not found")
	errUnauthorizedGame = errors.New("game belongs to another user")
	errRevisionConflict = errors.New("save revision conflict")
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email,omitempty"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Game struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SaveMetadata struct {
	GameID    string    `json:"game_id"`
	Name      string    `json:"name"`
	Version   int       `json:"version"`
	Revision  int       `json:"revision"`
	SizeBytes int64     `json:"size_bytes"`
	SavedAt   time.Time `json:"saved_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SaveRecord struct {
	SaveMetadata
	Data []byte
}

type dbStore struct {
	db *sql.DB
}

func newID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func openDB(dbPath string) (*dbStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Optimize SQLite settings for concurrent web access
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec %q: %w", pragma, err)
		}
	}

	// Limit connections for SQLite file locking
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	store := &dbStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	return store, nil
}

func (s *dbStore) Close() error {
	return s.db.Close()
}

func (s *dbStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS bug_reports (
		id TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE COLLATE NOCASE,
		email TEXT UNIQUE COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS tokens (
		token_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS games (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS saves (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		version INTEGER NOT NULL DEFAULT 1,
		revision INTEGER NOT NULL DEFAULT 1,
		data BLOB NOT NULL,
		size_bytes INTEGER NOT NULL,
		saved_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, game_id)
	);

	CREATE INDEX IF NOT EXISTS idx_games_user ON games(user_id, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_saves_user ON saves(user_id, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_saves_game ON saves(game_id);
	CREATE INDEX IF NOT EXISTS idx_tokens_user ON tokens(user_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *dbStore) CreateUser(ctx context.Context, username, email, passwordHash string) (*User, error) {
	id := newID()
	var emailVal *string
	if email != "" {
		emailVal = &email
	}

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, username, emailVal, passwordHash, now, now)

	if err != nil {
		// Detect unique constraint violation
		if isConstraintError(err) {
			return nil, errUserExists
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	return &User{
		ID:           id,
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *dbStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users
		WHERE username = ?
	`, username)

	var u User
	var emailVal sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &emailVal, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errUserNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	if emailVal.Valid {
		u.Email = emailVal.String
	}
	return &u, nil
}

func (s *dbStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, created_at, updated_at
		FROM users
		WHERE id = ?
	`, id)

	var u User
	var emailVal sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &emailVal, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errUserNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	if emailVal.Valid {
		u.Email = emailVal.String
	}
	return &u, nil
}

func (s *dbStore) CreateToken(ctx context.Context, tokenHash, userID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tokens (token_hash, user_id, created_at, last_used_at)
		VALUES (?, ?, ?, ?)
	`, tokenHash, userID, now, now)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	return nil
}

func (s *dbStore) GetUserByToken(ctx context.Context, tokenHash string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.created_at, u.updated_at
		FROM users u
		JOIN tokens t ON t.user_id = u.id
		WHERE t.token_hash = ?
	`, tokenHash)

	var u User
	var emailVal sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &emailVal, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errUserNotFound
		}
		return nil, fmt.Errorf("get user by token: %w", err)
	}
	if emailVal.Valid {
		u.Email = emailVal.String
	}

	// Update last_used_at asynchronously or inline
	_, _ = s.db.ExecContext(ctx, `
		UPDATE tokens SET last_used_at = ? WHERE token_hash = ?
	`, time.Now().UTC(), tokenHash)

	return &u, nil
}

func (s *dbStore) DeleteToken(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *dbStore) ListSaves(ctx context.Context, userID string) ([]SaveMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT game_id, name, version, revision, size_bytes, saved_at, updated_at
		FROM saves
		WHERE user_id = ?
		ORDER BY updated_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list saves: %w", err)
	}
	defer rows.Close()

	var saves []SaveMetadata
	for rows.Next() {
		var sm SaveMetadata
		if err := rows.Scan(&sm.GameID, &sm.Name, &sm.Version, &sm.Revision, &sm.SizeBytes, &sm.SavedAt, &sm.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan save metadata: %w", err)
		}
		saves = append(saves, sm)
	}
	if saves == nil {
		saves = []SaveMetadata{}
	}
	return saves, rows.Err()
}

func (s *dbStore) GetSave(ctx context.Context, userID, gameID string) (*SaveRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT game_id, name, version, revision, size_bytes, saved_at, updated_at, data
		FROM saves
		WHERE user_id = ? AND game_id = ?
	`, userID, gameID)

	var sr SaveRecord
	if err := row.Scan(
		&sr.GameID, &sr.Name, &sr.Version, &sr.Revision,
		&sr.SizeBytes, &sr.SavedAt, &sr.UpdatedAt, &sr.Data,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errSaveNotFound
		}
		return nil, fmt.Errorf("get save: %w", err)
	}
	return &sr, nil
}

func (s *dbStore) PutSave(ctx context.Context, userID string, save SaveRecord, expectedRevision *int) (*SaveMetadata, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// Ensure game exists and belongs to this user
	var gameOwner string
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM games WHERE id = ?`, save.GameID).Scan(&gameOwner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check game: %w", err)
	}

	if err == nil {
		if gameOwner != userID {
			return nil, errUnauthorizedGame
		}
		_, err = tx.ExecContext(ctx, `UPDATE games SET name = ?, updated_at = ? WHERE id = ?`, save.Name, now, save.GameID)
		if err != nil {
			return nil, fmt.Errorf("update game: %w", err)
		}
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO games (id, user_id, name, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, save.GameID, userID, save.Name, now, now)
		if err != nil {
			return nil, fmt.Errorf("create game: %w", err)
		}
	}

	var existingRev int
	err = tx.QueryRowContext(ctx, `
		SELECT revision FROM saves WHERE user_id = ? AND game_id = ?
	`, userID, save.GameID).Scan(&existingRev)

	exists := true
	if errors.Is(err, sql.ErrNoRows) {
		exists = false
	} else if err != nil {
		return nil, fmt.Errorf("check existing save: %w", err)
	}

	if expectedRevision != nil {
		if !exists && *expectedRevision != 0 {
			return nil, errRevisionConflict
		}
		if exists && existingRev != *expectedRevision {
			return nil, errRevisionConflict
		}
	}

	var newRev int
	if exists {
		newRev = existingRev + 1
		_, err = tx.ExecContext(ctx, `
			UPDATE saves
			SET name = ?, version = ?, revision = ?, data = ?, size_bytes = ?, saved_at = ?, updated_at = ?
			WHERE user_id = ? AND game_id = ?
		`, save.Name, save.Version, newRev, save.Data, save.SizeBytes, save.SavedAt, now, userID, save.GameID)
	} else {
		newRev = 1
		id := newID()
		_, err = tx.ExecContext(ctx, `
			INSERT INTO saves (id, game_id, user_id, name, version, revision, data, size_bytes, saved_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, save.GameID, userID, save.Name, save.Version, newRev, save.Data, save.SizeBytes, save.SavedAt, now)
	}

	if err != nil {
		return nil, fmt.Errorf("save record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit save tx: %w", err)
	}

	return &SaveMetadata{
		GameID:    save.GameID,
		Name:      save.Name,
		Version:   save.Version,
		Revision:  newRev,
		SizeBytes: save.SizeBytes,
		SavedAt:   save.SavedAt,
		UpdatedAt: now,
	}, nil
}

func (s *dbStore) ListGames(ctx context.Context, userID string) ([]Game, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, created_at, updated_at
		FROM games
		WHERE user_id = ?
		ORDER BY updated_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		if err := rows.Scan(&g.ID, &g.UserID, &g.Name, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		games = append(games, g)
	}
	if games == nil {
		games = []Game{}
	}
	return games, rows.Err()
}

func (s *dbStore) GetGame(ctx context.Context, userID, gameID string) (*Game, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, created_at, updated_at
		FROM games
		WHERE user_id = ? AND id = ?
	`, userID, gameID)

	var g Game
	if err := row.Scan(&g.ID, &g.UserID, &g.Name, &g.CreatedAt, &g.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errGameNotFound
		}
		return nil, fmt.Errorf("get game: %w", err)
	}
	return &g, nil
}

func (s *dbStore) DeleteGame(ctx context.Context, userID, gameID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM games WHERE user_id = ? AND id = ?`, userID, gameID)
	if err != nil {
		return false, fmt.Errorf("delete game: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (s *dbStore) DeleteSave(ctx context.Context, userID, gameID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM saves WHERE user_id = ? AND game_id = ?`, userID, gameID)
	if err != nil {
		return false, fmt.Errorf("delete save: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint failed")
}
