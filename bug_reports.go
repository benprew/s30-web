package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *dbStore) storeBugReport(ctx context.Context, report []byte) (string, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("create report ID: %w", err)
	}
	id := hex.EncodeToString(token)
	_, err := s.db.ExecContext(ctx, `INSERT INTO bug_reports (id, data) VALUES (?, ?)`, id, report)
	if err != nil {
		return "", fmt.Errorf("store bug report: %w", err)
	}
	return id, nil
}

func bugReportIssueBody(summary, reportURL string) string {
	// Old clients put the full report after this marker.
	if index := strings.Index(summary, "<details><summary><b>Full State JSON"); index >= 0 {
		summary = summary[:index]
	}
	summary = strings.TrimSpace(summary)
	if len([]rune(summary)) > maxBodyRunes {
		summary = truncateRunes(summary, maxBodyRunes) + "\n\n_Summary truncated. The download contains the full report._"
	}
	return summary + "\n\n[Download full report](" + reportURL + ")\n"
}

func bugReportDownloadHandler(store *dbStore, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, submitResult{Error: "Method not allowed"})
			return
		}
		id := r.PathValue("id")
		if _, err := hex.DecodeString(id); err != nil || len(id) != 32 {
			http.NotFound(w, r)
			return
		}
		var report []byte
		err := store.db.QueryRowContext(r.Context(), `SELECT data FROM bug_reports WHERE id = ?`, id).Scan(&report)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			logger.Error("could not read bug report", "error", err)
			writeJSON(w, http.StatusInternalServerError, submitResult{Error: "Could not read the report"})
			return
		}
		extension := ".json"
		contentType := "application/json"
		if isGzipReport(report) {
			extension = ".json.gz"
			contentType = "application/gzip"
		}
		filename := "report-" + id + extension
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(report))
	}
}

const maxReportMetadataBytes = 64 << 10

func isGzipReport(report []byte) bool {
	return len(report) >= 3 && bytes.Equal(report[:3], []byte{0x1f, 0x8b, 0x08})
}

func readBugReportMultipart(r *http.Request) (bugReportPayload, error) {
	var payload bugReportPayload
	reader, err := r.MultipartReader()
	if err != nil {
		return payload, err
	}
	var hasMetadata, hasReport bool
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return payload, err
		}
		switch part.FormName() {
		case "metadata":
			if hasMetadata {
				return payload, errors.New("duplicate metadata")
			}
			hasMetadata = true
			data, err := io.ReadAll(io.LimitReader(part, maxReportMetadataBytes+1))
			if err != nil {
				return payload, err
			}
			if len(data) > maxReportMetadataBytes {
				return payload, &http.MaxBytesError{Limit: maxReportMetadataBytes}
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				return payload, err
			}
		case "report":
			if hasReport {
				return payload, errors.New("duplicate report")
			}
			hasReport = true
			data, err := io.ReadAll(part)
			if err != nil {
				return payload, err
			}
			if !isGzipReport(data) {
				return payload, errors.New("report must be a gzip file")
			}
			payload.CompressedReport = data
		default:
			return payload, errors.New("unknown upload field")
		}
	}
	if !hasMetadata || !hasReport {
		return payload, errors.New("metadata and report are required")
	}
	return payload, nil
}
