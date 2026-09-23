package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxRequestBytes = 2_000_000
	maxTitleRunes   = 220
	maxBodyRunes    = 62_000
)

type config struct {
	address        string
	staticDir      string
	githubToken    string
	githubOwner    string
	githubRepo     string
	githubEndpoint string
}

type bugReportPayload struct {
	Title   string          `json:"title"`
	Body    string          `json:"body"`
	IsCrash bool            `json:"is_crash"`
	Report  json.RawMessage `json:"report"`
}

type githubIssue struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
}

type submitResult struct {
	Success     bool   `json:"success"`
	IssueURL    string `json:"issue_url,omitempty"`
	IssueNumber int    `json:"issue_number,omitempty"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
}

func configFromEnvironment() config {
	port := environmentOrDefault("PORT", "8080")
	return config{
		address:        ":" + port,
		staticDir:      environmentOrDefault("STATIC_DIR", "public"),
		githubToken:    os.Getenv("GITHUB_TOKEN"),
		githubOwner:    environmentOrDefault("GITHUB_REPO_OWNER", "benprew"),
		githubRepo:     environmentOrDefault("GITHUB_REPO_NAME", "s30"),
		githubEndpoint: environmentOrDefault("GITHUB_API_URL", "https://api.github.com"),
	}
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func newServer(cfg config, client *http.Client, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/api/bug-reports", bugReportHandler(cfg, client, logger))
	mux.Handle("/", staticHandler(cfg.staticDir))
	return securityHeaders(mux)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func bugReportHandler(cfg config, client *http.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, submitResult{Error: "Method not allowed"})
			return
		}

		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, submitResult{Error: "Content-Type must be application/json"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		decoder := json.NewDecoder(r.Body)
		var payload bugReportPayload
		if err := decoder.Decode(&payload); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeJSON(w, http.StatusRequestEntityTooLarge, submitResult{Error: "Report is too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, submitResult{Error: "Request body is not valid JSON"})
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, submitResult{Error: "Request body must contain one JSON object"})
			return
		}

		payload.Title = sanitizeTitle(payload.Title, payload.IsCrash)
		payload.Body = strings.TrimSpace(payload.Body)
		if payload.Body == "" {
			writeJSON(w, http.StatusBadRequest, submitResult{Error: "Report body is required"})
			return
		}
		bodyWasTruncated := utf8.RuneCountInString(payload.Body) > maxBodyRunes
		payload.Body = truncateRunes(payload.Body, maxBodyRunes)
		if bodyWasTruncated {
			payload.Body += "\n\n_Report truncated by the bug report service._"
		}

		if cfg.githubToken == "" {
			writeJSON(w, http.StatusServiceUnavailable, submitResult{Error: "Bug report service is not configured"})
			return
		}

		issue, err := createGitHubIssue(r.Context(), cfg, client, payload)
		if err != nil {
			logger.Error("bug report submission failed", "error", err)
			writeJSON(w, http.StatusBadGateway, submitResult{Error: "Could not create the issue"})
			return
		}

		writeJSON(w, http.StatusOK, submitResult{
			Success:     true,
			IssueURL:    issue.HTMLURL,
			IssueNumber: issue.Number,
			Message:     fmt.Sprintf("Created issue #%d", issue.Number),
		})
	}
}

func sanitizeTitle(title string, isCrash bool) string {
	fallback := "[Bug] In-game report"
	if isCrash {
		fallback = "[Crash] In-game crash report"
	}
	title = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, title)
	title = strings.TrimSpace(title)
	if title == "" {
		return fallback
	}
	return truncateRunes(title, maxTitleRunes)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func createGitHubIssue(ctx context.Context, cfg config, client *http.Client, payload bugReportPayload) (githubIssue, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	labels := []string{"in-app-report", "bug"}
	if payload.IsCrash {
		labels = []string{"in-app-report", "crash"}
	}
	requestBody, err := json.Marshal(map[string]any{
		"title":  payload.Title,
		"body":   payload.Body,
		"labels": labels,
	})
	if err != nil {
		return githubIssue{}, fmt.Errorf("encode GitHub request: %w", err)
	}

	endpoint := strings.TrimRight(cfg.githubEndpoint, "/") + "/repos/" +
		url.PathEscape(cfg.githubOwner) + "/" + url.PathEscape(cfg.githubRepo) + "/issues"
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, strings.NewReader(string(requestBody)))
	if err != nil {
		return githubIssue{}, fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+cfg.githubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "s30-bug-report-service/1.0")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := client.Do(req)
	if err != nil {
		return githubIssue{}, fmt.Errorf("send GitHub request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return githubIssue{}, fmt.Errorf("GitHub returned status %d", response.StatusCode)
	}

	var issue githubIssue
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&issue); err != nil {
		return githubIssue{}, fmt.Errorf("decode GitHub response: %w", err)
	}
	if issue.HTMLURL == "" || issue.Number <= 0 {
		return githubIssue{}, errors.New("GitHub returned an incomplete issue")
	}
	return issue, nil
}

func staticHandler(staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean("/" + r.URL.Path)
		if strings.Contains(cleanPath, "/.") {
			http.NotFound(w, r)
			return
		}

		target := filepath.Join(staticDir, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
		if cleanPath == "/" {
			target = filepath.Join(staticDir, "index.html")
		}
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(cleanPath, "/images/") || strings.HasSuffix(cleanPath, ".ttf") || strings.HasSuffix(cleanPath, ".svg") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else if strings.HasSuffix(cleanPath, ".css") || strings.HasSuffix(cleanPath, ".js") {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
