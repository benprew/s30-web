package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBugReportCreatesGitHubIssue(t *testing.T) {
	var githubRequest struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&githubRequest); err != nil {
			t.Fatalf("decode GitHub request: %v", err)
		}
		writeJSON(w, http.StatusCreated, githubIssue{HTMLURL: "https://github.test/issues/42", Number: 42})
	}))
	defer github.Close()

	handler := newTestServer(t, config{
		githubToken:    "test-token",
		githubOwner:    "benprew",
		githubRepo:     "s30",
		githubEndpoint: github.URL,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/bug-reports", strings.NewReader(`{
		"title":"[Crash] Test\nTitle",
		"body":"Crash details",
		"is_crash":true,
		"report":{"id":"report-1"}
	}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var result submitResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.Success || result.IssueNumber != 42 || result.IssueURL != "https://github.test/issues/42" {
		t.Fatalf("result = %#v", result)
	}
	if githubRequest.Title != "[Crash] Test Title" {
		t.Fatalf("title = %q", githubRequest.Title)
	}
	if strings.Join(githubRequest.Labels, ",") != "in-app-report,crash" {
		t.Fatalf("labels = %v", githubRequest.Labels)
	}
}

func TestBugReportRejectsInvalidRequests(t *testing.T) {
	handler := newTestServer(t, config{})
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{}`, status: http.StatusUnsupportedMediaType},
		{name: "invalid JSON", contentType: "application/json", body: `{`, status: http.StatusBadRequest},
		{name: "missing body", contentType: "application/json", body: `{"title":"Bug"}`, status: http.StatusBadRequest},
		{name: "not configured", contentType: "application/json", body: `{"body":"Details"}`, status: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/bug-reports", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.status, recorder.Body.String())
			}
		})
	}
}

func TestBugReportRejectsLargeBody(t *testing.T) {
	handler := newTestServer(t, config{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/bug-reports", strings.NewReader(`{"body":"`+strings.Repeat("a", maxRequestBytes)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestOptionsIncludesCORSHeaders(t *testing.T) {
	handler := newTestServer(t, config{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api/bug-reports", nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestStaticHandlerServesLandingPage(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<h1>Shandalar</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newServer(config{staticDir: staticDir}, nil, http.DefaultClient, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Shandalar") {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func newTestServer(t *testing.T, cfg config) http.Handler {
	t.Helper()
	if cfg.staticDir == "" {
		cfg.staticDir = t.TempDir()
		if err := os.WriteFile(filepath.Join(cfg.staticDir, "index.html"), []byte("ok"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return newServer(cfg, nil, http.DefaultClient, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
