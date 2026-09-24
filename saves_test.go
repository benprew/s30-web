package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTestApp(t *testing.T) (http.Handler, *dbStore) {
	t.Helper()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")
	store, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	cfg := config{
		staticDir: t.TempDir(),
		dbPath:    dbPath,
	}
	handler := newServer(cfg, store, http.DefaultClient, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return handler, store
}

func registerTestUser(t *testing.T, handler http.Handler, username, password, email string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
		"email":    email,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var res authResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	return res.User.ID, res.Token
}

func TestAuthRegisterAndLogin(t *testing.T) {
	handler, _ := setupTestApp(t)

	// 1. Register valid user
	userID, token := registerTestUser(t, handler, "planeswalker", "supersecret123", "walker@dominaria.test")
	if userID == "" || token == "" {
		t.Fatalf("expected non-empty user ID and token")
	}

	// 2. Duplicate registration fails
	dupBody, _ := json.Marshal(map[string]string{
		"username": "planeswalker",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(dupBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}

	// 3. Login with valid credentials
	loginBody, _ := json.Marshal(map[string]string{
		"username": "planeswalker",
		"password": "supersecret123",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var loginRes authResponse
	if err := json.NewDecoder(rec.Body).Decode(&loginRes); err != nil {
		t.Fatal(err)
	}
	if loginRes.Token == "" || loginRes.User.Username != "planeswalker" {
		t.Fatalf("unexpected login response: %#v", loginRes)
	}

	// 4. Login with invalid password fails
	badLoginBody, _ := json.Marshal(map[string]string{
		"username": "planeswalker",
		"password": "wrongpassword",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(badLoginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// 5. Test /api/auth/me
	req = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// 6. Test /api/auth/logout
	req = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// 7. Verify logged out token is rejected
	req = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestSaveLifecycleAndConflictDetection(t *testing.T) {
	handler, store := setupTestApp(t)
	_, token := registerTestUser(t, handler, "archmage", "archmage1234", "archmage@shandalar.test")

	// Read actual sample save file
	sampleBytes, err := os.ReadFile("Apprentice-Red-ed18e335d8f4_2026-09-20_22-50-01.json")
	if err != nil {
		t.Fatalf("read sample save file: %v", err)
	}

	const gameID = "ed18e335d8f4"

	// 1. Initial list should be empty
	req := httptest.NewRequest(http.MethodGet, "/api/saves", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var listRes savesListResponse
	if err := json.NewDecoder(rec.Body).Decode(&listRes); err != nil {
		t.Fatal(err)
	}
	if len(listRes.Saves) != 0 {
		t.Fatalf("expected 0 saves, got %d", len(listRes.Saves))
	}

	// 2. Upload sample save
	req = httptest.NewRequest(http.MethodPut, "/api/saves/"+gameID, bytes.NewReader(sampleBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var putRes saveResponse
	if err := json.NewDecoder(rec.Body).Decode(&putRes); err != nil {
		t.Fatal(err)
	}
	if !putRes.Success || putRes.Save.Revision != 1 || putRes.Save.SizeBytes != int64(len(sampleBytes)) {
		t.Fatalf("unexpected put response: %#v", putRes)
	}

	// Verify foreign keys via SQLite PRAGMA
	rows, err := store.db.Query("PRAGMA foreign_key_check;")
	if err != nil {
		t.Fatalf("foreign key check failed: %v", err)
	}
	if rows.Next() {
		t.Fatalf("foreign key violation detected in database!")
	}
	_ = rows.Close()

	// 3. Verify game was automatically created in games table
	req = httptest.NewRequest(http.MethodGet, "/api/games", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list games status = %d", rec.Code)
	}
	var gamesRes gamesListResponse
	if err := json.NewDecoder(rec.Body).Decode(&gamesRes); err != nil {
		t.Fatal(err)
	}
	if len(gamesRes.Games) != 1 || gamesRes.Games[0].ID != gameID {
		t.Fatalf("expected 1 game with ID %s, got %#v", gameID, gamesRes)
	}

	// 4. Download save as uncompressed JSON
	req = httptest.NewRequest(http.MethodGet, "/api/saves/"+gameID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get save status = %d", rec.Code)
	}
	if rec.Header().Get("X-Save-Revision") != "1" {
		t.Fatalf("X-Save-Revision = %q, want 1", rec.Header().Get("X-Save-Revision"))
	}
	if !bytes.Equal(rec.Body.Bytes(), sampleBytes) {
		t.Fatalf("downloaded bytes mismatch with original save file")
	}

	// 5. Download save with Accept-Encoding: zstd
	req = httptest.NewRequest(http.MethodGet, "/api/saves/"+gameID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept-Encoding", "zstd")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get zstd status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "zstd" {
		t.Fatalf("Content-Encoding = %q, want zstd", rec.Header().Get("Content-Encoding"))
	}
	decompressed, err := zstdDecompress(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("read decompressed zstd: %v", err)
	}
	if !bytes.Equal(decompressed, sampleBytes) {
		t.Fatalf("decompressed zstd bytes mismatch")
	}

	// 6. Update save with matching If-Match: 1 -> should succeed and increment revision to 2
	req = httptest.NewRequest(http.MethodPut, "/api/saves/"+gameID, bytes.NewReader(sampleBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", "1")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", rec.Code, rec.Body.String())
	}
	putRes = saveResponse{}
	_ = json.NewDecoder(rec.Body).Decode(&putRes)
	if putRes.Save.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", putRes.Save.Revision)
	}

	// 7. Update with stale If-Match: 1 -> should fail with 409 Conflict
	req = httptest.NewRequest(http.MethodPut, "/api/saves/"+gameID, bytes.NewReader(sampleBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", "1")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d", rec.Code, http.StatusConflict)
	}
	var conflictRes saveResponse
	_ = json.NewDecoder(rec.Body).Decode(&conflictRes)
	if conflictRes.CurrentRevision != 2 {
		t.Fatalf("expected CurrentRevision 2, got %d", conflictRes.CurrentRevision)
	}

	// 8. Delete save
	req = httptest.NewRequest(http.MethodDelete, "/api/saves/"+gameID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}

	// 9. Subsequent GET returns 404
	req = httptest.NewRequest(http.MethodGet, "/api/saves/"+gameID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestUserIsolationAndForeignKeys(t *testing.T) {
	handler, _ := setupTestApp(t)
	_, user1Token := registerTestUser(t, handler, "player1", "pass123456", "p1@test.com")
	_, user2Token := registerTestUser(t, handler, "player2", "pass123456", "p2@test.com")

	saveJSON := []byte(`{"name":"Run1","game_id":"game123","version":1,"world":{}}`)

	// Player 1 uploads save
	req := httptest.NewRequest(http.MethodPut, "/api/saves/game123", bytes.NewReader(saveJSON))
	req.Header.Set("Authorization", "Bearer "+user1Token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("player1 upload status = %d", rec.Code)
	}

	// Player 2 listing should be empty
	req = httptest.NewRequest(http.MethodGet, "/api/saves", nil)
	req.Header.Set("Authorization", "Bearer "+user2Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var listRes savesListResponse
	_ = json.NewDecoder(rec.Body).Decode(&listRes)
	if len(listRes.Saves) != 0 {
		t.Fatalf("expected player2 to have 0 saves, got %d", len(listRes.Saves))
	}

	// Player 2 cannot save over Player 1's game -> 403 Forbidden
	req = httptest.NewRequest(http.MethodPut, "/api/saves/game123", bytes.NewReader(saveJSON))
	req.Header.Set("Authorization", "Bearer "+user2Token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when player2 uploads to player1 game, got %d", rec.Code)
	}

	// Player 2 cannot GET player 1's save
	req = httptest.NewRequest(http.MethodGet, "/api/saves/game123", nil)
	req.Header.Set("Authorization", "Bearer "+user2Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for player2 accessing player1 save, got %d", rec.Code)
	}

	// Player 2 cannot DELETE player 1's game
	req = httptest.NewRequest(http.MethodDelete, "/api/games/game123", nil)
	req.Header.Set("Authorization", "Bearer "+user2Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for player2 deleting player1 game, got %d", rec.Code)
	}

	// Player 1 deletes game -> cascade deletes save
	req = httptest.NewRequest(http.MethodDelete, "/api/games/game123", nil)
	req.Header.Set("Authorization", "Bearer "+user1Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("player1 delete game failed: %d", rec.Code)
	}

	// Verify save is gone too
	req = httptest.NewRequest(http.MethodGet, "/api/saves/game123", nil)
	req.Header.Set("Authorization", "Bearer "+user1Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for save after game deleted, got %d", rec.Code)
	}
}

func TestZstdUpload(t *testing.T) {
	handler, _ := setupTestApp(t)
	_, token := registerTestUser(t, handler, "compressor", "password123", "")

	rawJSON := []byte(`{"name":"SaveWithZstd","game_id":"zstd123","version":1}`)
	compressed, err := zstdCompress(rawJSON)
	if err != nil {
		t.Fatalf("compress zstd: %v", err)
	}

	// Upload pre-compressed zstd payload
	req := httptest.NewRequest(http.MethodPut, "/api/saves/zstd123", bytes.NewReader(compressed))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload zstd status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Download uncompressed
	req = httptest.NewRequest(http.MethodGet, "/api/saves/zstd123", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), rawJSON) {
		t.Fatalf("download mismatch: got %q, want %q", rec.Body.String(), string(rawJSON))
	}
}

func TestGzipUploadBackwardsCompatibility(t *testing.T) {
	handler, _ := setupTestApp(t)
	_, token := registerTestUser(t, handler, "legacyclient", "password123", "")

	rawJSON := []byte(`{"name":"SaveLegacy","game_id":"legacy123","version":1}`)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(rawJSON)
	_ = gw.Close()

	// Upload gzip payload
	req := httptest.NewRequest(http.MethodPut, "/api/saves/legacy123", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload gzip status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Download uncompressed
	req = httptest.NewRequest(http.MethodGet, "/api/saves/legacy123", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), rawJSON) {
		t.Fatalf("download mismatch")
	}
}
