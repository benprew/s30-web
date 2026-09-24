package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]{3,32}$`)
)

type contextKey string

const userCtxKey = contextKey("auth_user")

type authResponse struct {
	Success bool   `json:"success"`
	User    *User  `json:"user,omitempty"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func generateToken() (string, string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(bytes)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func userFromContext(ctx context.Context) *User {
	if u, ok := ctx.Value(userCtxKey).(*User); ok {
		return u
	}
	return nil
}

func authMiddleware(store *dbStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, authResponse{Error: "Missing or invalid authorization header"})
				return
			}

			rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			if rawToken == "" {
				writeJSON(w, http.StatusUnauthorized, authResponse{Error: "Empty token"})
				return
			}

			user, err := store.GetUserByToken(r.Context(), hashToken(rawToken))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, authResponse{Error: "Invalid or expired session token"})
				return
			}

			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func registerHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, authResponse{Error: "Method not allowed"})
			return
		}

		var req registerRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, authResponse{Error: "Invalid JSON request body"})
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		req.Email = strings.TrimSpace(req.Email)

		if !usernameRegex.MatchString(req.Username) {
			writeJSON(w, http.StatusBadRequest, authResponse{
				Error: "Username must be 3-32 characters long and contain only letters, numbers, underscores, dashes, or dots",
			})
			return
		}

		if len(req.Password) < 8 || len(req.Password) > 128 {
			writeJSON(w, http.StatusBadRequest, authResponse{
				Error: "Password must be between 8 and 128 characters",
			})
			return
		}

		if req.Email != "" && (!strings.Contains(req.Email, "@") || len(req.Email) > 254) {
			writeJSON(w, http.StatusBadRequest, authResponse{Error: "Invalid email address"})
			return
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, authResponse{Error: "Could not hash password"})
			return
		}

		user, err := store.CreateUser(r.Context(), req.Username, req.Email, string(passwordHash))
		if err != nil {
			if errors.Is(err, errUserExists) {
				writeJSON(w, http.StatusConflict, authResponse{Error: "Username or email is already taken"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, authResponse{Error: "Could not create user account"})
			return
		}

		token, tokenHash, err := generateToken()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, authResponse{Error: "Could not generate authentication token"})
			return
		}

		if err := store.CreateToken(r.Context(), tokenHash, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, authResponse{Error: "Could not create session"})
			return
		}

		writeJSON(w, http.StatusCreated, authResponse{
			Success: true,
			User:    user,
			Token:   token,
		})
	}
}

func loginHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, authResponse{Error: "Method not allowed"})
			return
		}

		var req loginRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, authResponse{Error: "Invalid JSON request body"})
			return
		}

		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			writeJSON(w, http.StatusBadRequest, authResponse{Error: "Username and password are required"})
			return
		}

		user, err := store.GetUserByUsername(r.Context(), req.Username)
		if err != nil {
			// Don't leak whether user exists
			writeJSON(w, http.StatusUnauthorized, authResponse{Error: "Invalid username or password"})
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			writeJSON(w, http.StatusUnauthorized, authResponse{Error: "Invalid username or password"})
			return
		}

		token, tokenHash, err := generateToken()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, authResponse{Error: "Could not generate authentication token"})
			return
		}

		if err := store.CreateToken(r.Context(), tokenHash, user.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, authResponse{Error: "Could not create session"})
			return
		}

		writeJSON(w, http.StatusOK, authResponse{
			Success: true,
			User:    user,
			Token:   token,
		})
	}
}

func logoutHandler(store *dbStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, authResponse{Error: "Method not allowed"})
			return
		}

		authHeader := r.Header.Get("Authorization")
		rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if rawToken != "" {
			_ = store.DeleteToken(r.Context(), hashToken(rawToken))
		}

		writeJSON(w, http.StatusOK, authResponse{
			Success: true,
			Message: "Logged out successfully",
		})
	}
}

func meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, authResponse{Error: "Method not allowed"})
			return
		}

		user := userFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, authResponse{Error: "Unauthorized"})
			return
		}

		writeJSON(w, http.StatusOK, authResponse{
			Success: true,
			User:    user,
		})
	}
}
