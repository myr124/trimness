// Package session defines persisted chat sessions and a JSON-file store.
package session

import (
	"strings"
	"time"
	"trimness/internal/core"

	"github.com/google/uuid"
)

const titleMax = 40

type Session struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Messages  []core.Message `json:"messages"`
}

// Summary is the lightweight view used by the session picker.
type Summary struct {
	ID        string
	Title     string
	UpdatedAt time.Time
}

// Store persists sessions. Implementations must make Save atomic.
type Store interface {
	Save(Session) error
	Load(id string) (Session, error)
	List() ([]Summary, error)
}

// New creates an empty session with a fresh unique ID.
func New() Session {
	now := time.Now()
	return Session{ID: uuid.NewString(), Title: "New session", CreatedAt: now, UpdatedAt: now}
}

// DeriveTitle sets the title from the first user message, if there is one.
func (s *Session) DeriveTitle() {
	for _, m := range s.Messages {
		if m.Role != "user" {
			continue
		}
		t := strings.Join(strings.Fields(m.Content), " ")
		if r := []rune(t); len(r) > titleMax {
			t = string(r[:titleMax]) + "…"
		}
		if t != "" {
			s.Title = t
		}
		return
	}
}
