package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func reportUploadRequest(t *testing.T, file []byte, metadata string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("metadata", metadata); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("report", "report.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/bug-reports", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	return req
}

func TestBugReportStoresAndServesGzipUnchanged(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, githubIssue{HTMLURL: "https://github.test/issues/42", Number: 42})
	}))
	defer github.Close()
	handler := newTestServer(t, config{githubToken: "token", githubEndpoint: github.URL})
	compact := []byte(`{"state":"` + strings.Repeat("world data ", 300000) + `"}`)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(compact); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, reportUploadRequest(t, compressed.Bytes(), `{"title":"Bug","body":"Summary"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	var result submitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	download := httptest.NewRecorder()
	handler.ServeHTTP(download, httptest.NewRequest(http.MethodGet, result.ReportURL, nil))
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), compressed.Bytes()) {
		t.Fatalf("download status=%d; gzip bytes changed", download.Code)
	}
	if download.Header().Get("Content-Type") != "application/gzip" || !strings.Contains(download.Header().Get("Content-Disposition"), ".json.gz") {
		t.Fatal("download is not a gzip file")
	}
	if download.Header().Get("Content-Encoding") != "" {
		t.Fatal("download must not use transparent HTTP decompression")
	}
	reader, err := gzip.NewReader(download.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, compact) {
		t.Fatal("download lost report data")
	}
}

func TestBugReportUploadRejectsInvalidFiles(t *testing.T) {
	handler := newTestServer(t, config{githubToken: "token"})
	for _, tc := range []struct {
		name     string
		file     []byte
		metadata string
		status   int
	}{
		{"not gzip", []byte(`{"state":1}`), `{"body":"summary"}`, http.StatusBadRequest},
		{"bad metadata", []byte{31, 139, 8}, `{`, http.StatusBadRequest},
		{"large metadata", []byte{31, 139, 8}, `{"body":"` + strings.Repeat("x", maxReportMetadataBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"large file", append([]byte{31, 139, 8}, bytes.Repeat([]byte("x"), maxRequestBytes)...), `{"body":"summary"}`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, reportUploadRequest(t, tc.file, tc.metadata))
			if rec.Code != tc.status {
				t.Errorf("status=%d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}
