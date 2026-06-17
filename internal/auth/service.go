package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"reni-social/internal/domain"
	"reni-social/internal/utils"
	"reni-social/pkg/db"
)

const sessionTTL = 24 * time.Hour

// ──────────────────────────────────────────────────────────────────────────────
// Signup
// ──────────────────────────────────────────────────────────────────────────────

type SignupRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  *domain.User `json:"user"`
}

func Signup(ctx context.Context, req SignupRequest) (*AuthResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, fmt.Errorf("email and password are required")
	}
	if len(req.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Check email uniqueness
	var exists bool
	err := db.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, req.Email).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	userID := uuid.New()
	now := time.Now()

	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, first_name, last_name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, req.Email, string(hash), req.FirstName, req.LastName, now)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	token, err := issueSession(userID)
	if err != nil {
		return nil, err
	}

	return &AuthResponse{
		Token: token,
		User: &domain.User{
			ID:        userID,
			Email:     req.Email,
			FirstName: req.FirstName,
			LastName:  req.LastName,
			CreatedAt: now,
		},
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Signin
// ──────────────────────────────────────────────────────────────────────────────

type SigninRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func Signin(ctx context.Context, req SigninRequest) (*AuthResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, fmt.Errorf("email and password are required")
	}

	var user domain.User
	err := db.DB.QueryRowContext(ctx, `
		SELECT id, email, password_hash, first_name, last_name, avatar_url, created_at
		FROM users WHERE email = $1
	`, req.Email).Scan(
		&user.ID, &user.Email, &user.PasswordHash,
		&user.FirstName, &user.LastName, &user.AvatarURL, &user.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid email or password")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	token, err := issueSession(user.ID)
	if err != nil {
		return nil, err
	}

	user.PasswordHash = "" // never expose
	return &AuthResponse{Token: token, User: &user}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Signout
// ──────────────────────────────────────────────────────────────────────────────

func Signout(token string) error {
	return utils.DeleteSession(token)
}

// ──────────────────────────────────────────────────────────────────────────────
// Me
// ──────────────────────────────────────────────────────────────────────────────

func GetMe(ctx context.Context, userID string) (*domain.User, error) {
	var user domain.User
	err := db.DB.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name, avatar_url, created_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&user.ID, &user.Email,
		&user.FirstName, &user.LastName, &user.AvatarURL, &user.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	return &user, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Session helpers
// ──────────────────────────────────────────────────────────────────────────────

// issueSession generates a cryptographically random token, stores it in Redis,
// and returns the token to be sent to the client.
func issueSession(userID uuid.UUID) (string, error) {
	token := utils.RandomAlphaNumeric(64)
	if err := utils.SetSession(token, userID, sessionTTL); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}
