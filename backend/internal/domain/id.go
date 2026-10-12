// Package domain holds the domain models shared by every layer.
// It MUST NOT depend on any other internal package.
package domain

import "github.com/google/uuid"

// NewID returns a new time-ordered (UUIDv7) identifier used as a TEXT primary key.
func NewID() string { return uuid.Must(uuid.NewV7()).String() }
