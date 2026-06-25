package domain

import (
	"time"

	"github.com/google/uuid"
)

// User represents an authenticated platform user.
type User struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	PasswordHash    string    `json:"-"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	FullName        string    `json:"fullname"`
	PhoneNumber     string    `json:"phone_number"`
	Username        string    `json:"username"`
	Gender          string    `json:"gender"`
	Address         string    `json:"address"`
	AvatarURL       string    `json:"avatar_url"`
	Token           string    `json:"token"`
	IsEmailVerified bool      `json:"is_email_verified"`
	CreatedAt       time.Time `json:"created_at"`
}
