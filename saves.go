package main

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const maxSaveBytes = 10_000_000

var (
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zstdDecoder, _ = zstd.NewReader(nil)
)

type saveEnvelope struct {
	Name    string `json:"name"`
	GameID  string `json:"game_id"`
	Version int    `json:"version"`
	SavedAt string `json:"saved_at"`
}

type savesListResponse struct {
	Success bool           `json:"success"`
	Saves   []SaveMetadata `json:"saves"`
	Error   string         `json:"error,omitempty"`
}

type gamesListResponse struct {
	Success bool   `json:"success"`
	Games   []Game `json:"games"`
	Error   string `json:"error,omitempty"`
}

type gameDetailResponse struct {
	Success bool          `json:"success"`
	Game    *Game         `json:"game,omitempty"`
	Save    *SaveMetadata `json:"save,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type saveResponse struct {
	Success         bool          `json:"success"`
	Save            *SaveMetadata `json:"save,omitempty"`
	CurrentRevision int           `json:"current_revision,omitempty"`
	Message         string        `json:"message,omitempty"`
	Error           string        `json:"error,omitempty"`
}

func zstdCompress(data []byte) ([]byte, error) {
	return zstdEncoder.EncodeAll(data, make([]byte, 0, len(data)/2)), nil
}

func zstdDecompress(data []byte) ([]byte, error) {
	return zstdDecoder.DecodeAll(data, nil)
}

func extractGameID(r *http.Request) string {
	if val := r.PathValue("game_id"); val != "" {
		return val
	}
	base := path.Base(r.URL.Path)
	if base == "saves" || base == "games" || base == "/" || base == "." {
		return ""
	}
	return base
}

func listGamesHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, gamesListResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, gamesListResponse{Error: "Unauthorized"})
			return
		}

		games, err := store.ListGames(r.Context(), user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, gamesListResponse{Error: "Could not list games"})
			return
		}

		writeJSON(w, http.StatusOK, gamesListResponse{
			Success: true,
			Games:   games,
		})
	}
}

func getGameHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, gameDetailResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, gameDetailResponse{Error: "Unauthorized"})
			return
		}

		gameID := extractGameID(r)
		if gameID == "" {
			writeJSON(w, http.StatusBadRequest, gameDetailResponse{Error: "Missing game ID"})
			return
		}

		game, err := store.GetGame(r.Context(), user.ID, gameID)
		if err != nil {
			if errors.Is(err, errGameNotFound) {
				writeJSON(w, http.StatusNotFound, gameDetailResponse{Error: "Game not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, gameDetailResponse{Error: "Could not retrieve game"})
			return
		}

		var saveMeta *SaveMetadata
		if save, err := store.GetSave(r.Context(), user.ID, gameID); err == nil {
			saveMeta = &save.SaveMetadata
		}

		writeJSON(w, http.StatusOK, gameDetailResponse{
			Success: true,
			Game:    game,
			Save:    saveMeta,
		})
	}
}

func deleteGameHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, saveResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, saveResponse{Error: "Unauthorized"})
			return
		}

		gameID := extractGameID(r)
		if gameID == "" {
			writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Missing game ID in path"})
			return
		}

		deleted, err := store.DeleteGame(r.Context(), user.ID, gameID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, saveResponse{Error: "Could not delete game"})
			return
		}
		if !deleted {
			writeJSON(w, http.StatusNotFound, saveResponse{Error: "Game not found"})
			return
		}

		writeJSON(w, http.StatusOK, saveResponse{
			Success: true,
			Message: "Game and associated saves deleted successfully",
		})
	}
}

func listSavesHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, savesListResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, savesListResponse{Error: "Unauthorized"})
			return
		}

		saves, err := store.ListSaves(r.Context(), user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, savesListResponse{Error: "Could not list saves"})
			return
		}

		writeJSON(w, http.StatusOK, savesListResponse{
			Success: true,
			Saves:   saves,
		})
	}
}

func getSaveHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, saveResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, saveResponse{Error: "Unauthorized"})
			return
		}

		gameID := extractGameID(r)
		if gameID == "" {
			writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Missing game ID"})
			return
		}

		save, err := store.GetSave(r.Context(), user.ID, gameID)
		if err != nil {
			if errors.Is(err, errSaveNotFound) {
				writeJSON(w, http.StatusNotFound, saveResponse{Error: "Save not found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, saveResponse{Error: "Could not retrieve save"})
			return
		}

		w.Header().Set("X-Save-Revision", strconv.Itoa(save.Revision))
		w.Header().Set("X-Save-Name", save.Name)
		w.Header().Set("X-Save-Version", strconv.Itoa(save.Version))
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, save.Revision))

		acceptEncoding := r.Header.Get("Accept-Encoding")
		if strings.Contains(acceptEncoding, "zstd") {
			// Serve the pre-compressed zstd BLOB directly
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Content-Encoding", "zstd")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(save.Data)
			return
		}

		// Client does not support zstd: decompress
		uncompressed, err := zstdDecompress(save.Data)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, saveResponse{Error: "Could not decompress save data"})
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(uncompressed)
	}
}

func putSaveHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			w.Header().Set("Allow", "PUT, POST, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, saveResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, saveResponse{Error: "Unauthorized"})
			return
		}

		gameID := extractGameID(r)
		if gameID == "" {
			writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Missing game ID in path"})
			return
		}

		bodyReader := http.MaxBytesReader(w, r.Body, maxSaveBytes)
		var rawBody []byte
		var err error

		contentEncoding := r.Header.Get("Content-Encoding")
		if strings.Contains(contentEncoding, "zstd") {
			// Client sent zstd compressed body
			compressedBytes, readErr := io.ReadAll(bodyReader)
			if readErr != nil {
				writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Could not read compressed body"})
				return
			}
			rawBody, err = zstdDecompress(compressedBytes)
		} else if strings.Contains(contentEncoding, "gzip") {
			// Client sent gzip compressed body (backwards compatibility)
			gzReader, gzErr := gzip.NewReader(bodyReader)
			if gzErr != nil {
				writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Invalid gzip stream"})
				return
			}
			rawBody, err = io.ReadAll(gzReader)
			_ = gzReader.Close()
		} else {
			rawBody, err = io.ReadAll(bodyReader)
		}

		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeJSON(w, http.StatusRequestEntityTooLarge, saveResponse{Error: "Save payload too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Could not read save payload"})
			return
		}

		// Inspect JSON envelope
		var envelope saveEnvelope
		if err := json.Unmarshal(rawBody, &envelope); err != nil {
			writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Payload is not valid JSON"})
			return
		}

		if envelope.GameID == "" {
			envelope.GameID = gameID
		}
		if envelope.Name == "" {
			envelope.Name = gameID
		}
		if envelope.Version <= 0 {
			envelope.Version = 1
		}

		var savedAt time.Time
		if envelope.SavedAt != "" {
			parsedTime, parseErr := time.Parse(time.RFC3339Nano, envelope.SavedAt)
			if parseErr == nil {
				savedAt = parsedTime
			} else {
				parsedTime, parseErr = time.Parse(time.RFC3339, envelope.SavedAt)
				if parseErr == nil {
					savedAt = parsedTime
				} else {
					savedAt = time.Now().UTC()
				}
			}
		} else {
			savedAt = time.Now().UTC()
		}

		// Check for revision precondition (If-Match or X-Save-Revision)
		var expectedRev *int
		revHeader := r.Header.Get("If-Match")
		if revHeader == "" {
			revHeader = r.Header.Get("X-Save-Revision")
		}
		if revHeader != "" {
			trimmed := strings.Trim(revHeader, `" `)
			if revNum, parseErr := strconv.Atoi(trimmed); parseErr == nil {
				expectedRev = &revNum
			}
		}

		// Compress body using zstandard for storage
		compressed, err := zstdCompress(rawBody)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, saveResponse{Error: "Could not compress save payload"})
			return
		}

		record := SaveRecord{
			SaveMetadata: SaveMetadata{
				GameID:    gameID,
				Name:      envelope.Name,
				Version:   envelope.Version,
				SizeBytes: int64(len(rawBody)),
				SavedAt:   savedAt,
			},
			Data: compressed,
		}

		meta, err := store.PutSave(r.Context(), user.ID, record, expectedRev)
		if err != nil {
			if errors.Is(err, errUnauthorizedGame) {
				writeJSON(w, http.StatusForbidden, saveResponse{
					Error: "Game belongs to another user",
				})
				return
			}
			if errors.Is(err, errRevisionConflict) {
				existing, getErr := store.GetSave(r.Context(), user.ID, gameID)
				var currentRev int
				if getErr == nil {
					currentRev = existing.Revision
				}
				writeJSON(w, http.StatusConflict, saveResponse{
					Error:           "Save revision conflict. Another client has uploaded a newer revision.",
					CurrentRevision: currentRev,
				})
				return
			}
			writeJSON(w, http.StatusInternalServerError, saveResponse{Error: "Could not write save to database"})
			return
		}

		w.Header().Set("X-Save-Revision", strconv.Itoa(meta.Revision))
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, meta.Revision))
		writeJSON(w, http.StatusOK, saveResponse{
			Success: true,
			Save:    meta,
		})
	}
}

func deleteSaveHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodDelete {
			w.Header().Set("Allow", "DELETE, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, saveResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, saveResponse{Error: "Unauthorized"})
			return
		}

		gameID := extractGameID(r)
		if gameID == "" {
			writeJSON(w, http.StatusBadRequest, saveResponse{Error: "Missing game ID in path"})
			return
		}

		deleted, err := store.DeleteSave(r.Context(), user.ID, gameID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, saveResponse{Error: "Could not delete save"})
			return
		}
		if !deleted {
			writeJSON(w, http.StatusNotFound, saveResponse{Error: "Save not found"})
			return
		}

		writeJSON(w, http.StatusOK, saveResponse{
			Success: true,
			Message: "Save deleted successfully",
		})
	}
}
