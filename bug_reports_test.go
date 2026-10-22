package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBugReportStoresFullReportAndLinksIssue(t *testing.T) {
	for _, failGitHub := range []bool{false, true} {
		name := "success"
		if failGitHub {
			name = "github failure"
		}
		t.Run(name, func(t *testing.T) {
			var body string
			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Body string `json:"body"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				body = payload.Body
				if failGitHub {
					http.Error(w, "unavailable", http.StatusBadGateway)
					return
				}
				writeJSON(w, http.StatusCreated, githubIssue{HTMLURL: "https://github.test/issues/42", Number: 42})
			}))
			defer github.Close()
			dbPath := filepath.Join(t.TempDir(), "reports.db")
			store, err := openDB(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg := config{githubToken: "test-token", githubEndpoint: github.URL, publicBaseURL: "https://s30.test"}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := newServer(cfg, store, http.DefaultClient, logger)
			report := json.RawMessage(`{"id":"client-id","world_state":"` + strings.Repeat("x", 3<<20) + `"}`)
			payload, _ := json.Marshal(bugReportPayload{Title: "Bug", Body: "Player description\n<details><summary><b>Full State JSON (click to expand)</b></summary>\nshould not be in the issue", Report: report})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/bug-reports", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(rec, req)
			wantStatus := http.StatusOK
			if failGitHub {
				wantStatus = http.StatusBadGateway
			}
			if rec.Code != wantStatus {
				t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
			}
			var result submitResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(result.ReportURL, "https://s30.test/api/bug-reports/") {
				t.Fatalf("download URL = %q", result.ReportURL)
			}
			if !strings.Contains(body, "Player description") || !strings.Contains(body, result.ReportURL) || strings.Contains(body, "should not be in the issue") {
				t.Fatalf("unexpected issue summary: %.500s", body)
			}
			if utf8.RuneCountInString(body) > 62000 {
				t.Fatal("issue exceeds GitHub limit")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = openDB(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			handler = newServer(cfg, store, http.DefaultClient, logger)
			download := httptest.NewRecorder()
			handler.ServeHTTP(download, httptest.NewRequest(http.MethodGet, result.ReportURL, nil))
			if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), report) {
				t.Fatalf("download status=%d, bytes=%d; want original report", download.Code, download.Body.Len())
			}
			if !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
				t.Fatal("download is not an attachment")
			}
		})
	}
}

func TestBugReportDownloadRejectsUnknownIDsAndMethods(t *testing.T) {
	handler := newTestServer(t, config{})
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/bug-reports/unknown", http.StatusNotFound},
		{http.MethodGet, "/api/bug-reports/00000000000000000000000000000000", http.StatusNotFound},
		{http.MethodPost, "/api/bug-reports/00000000000000000000000000000000", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.status {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, rec.Code, tc.status)
		}
	}
}

func TestReportStorageFailureDoesNotCreateIssue(t *testing.T) {
	called := false
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer github.Close()
	store, err := openDB(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	handler := newServer(config{githubToken: "token", githubEndpoint: github.URL}, store, http.DefaultClient, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/bug-reports", strings.NewReader(`{"body":"summary","report":{"id":"one"}}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || called {
		t.Fatalf("status=%d, GitHub called=%v", rec.Code, called)
	}
}

func TestReportIDsDoNotOverwriteClientReports(t *testing.T) {
	store, err := openDB(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.storeBugReport(context.Background(), []byte(`{"id":"same","notes":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.storeBugReport(context.Background(), []byte(`{"id":"same","notes":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("report IDs collide")
	}
}

func TestBugReportSummaryKeepsDownloadLinkAfterTruncation(t *testing.T) {
	link := "https://s30.test/api/bug-reports/0123456789abcdef0123456789abcdef"
	body := bugReportIssueBody(strings.Repeat("界", 100_000), link)
	if utf8.RuneCountInString(body) > 62000 || !strings.Contains(body, link) || !strings.Contains(body, "Summary truncated") {
		t.Fatal("large summary lost the link or exceeded the issue limit")
	}
}

func TestBugReportRequiresFullReportObject(t *testing.T) {
	handler := newTestServer(t, config{githubToken: "test-token"})
	for _, report := range []string{"null", "[]", `"text"`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/bug-reports", strings.NewReader(`{"body":"description","report":`+report+`}`))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("report=%s, status=%d", report, rec.Code)
		}
	}
}
