package domain

import (
	"time"

	"github.com/google/uuid"
)

// User represents an authenticated platform user.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
	AvatarURL    string
	CreatedAt    time.Time
}
