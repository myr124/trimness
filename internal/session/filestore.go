package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileStore stores one JSON file per session in Dir.
type FileStore struct {
	Dir string
}

// DefaultDir returns $XDG_DATA_HOME/trimness/sessions, falling back to
// ~/.local/share/trimness/sessions.
func DefaultDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "trimness", "sessions"), nil
}

func (fs FileStore) path(id string) (string, error) {
	// IDs come from uuid but may also arrive via a CLI flag; refuse path tricks.
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	return filepath.Join(fs.Dir, id+".json"), nil
}

// Save writes the session atomically: temp file in the same dir, then rename.
func (fs FileStore) Save(s Session) error {
	p, err := fs.path(s.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		cleanup()
		return err
	}
	return nil
}

func (fs FileStore) Load(id string) (Session, error) {
	p, err := fs.path(id)
	if err != nil {
		return Session{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, fmt.Errorf("session %s is corrupt: %w", id, err)
	}
	return s, nil
}

// List returns summaries sorted by UpdatedAt descending. Unreadable or corrupt
// files are skipped so one bad file doesn't hide every session.
func (fs FileStore) List() ([]Summary, error) {
	entries, err := os.ReadDir(fs.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		s, err := fs.Load(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		out = append(out, Summary{ID: s.ID, Title: s.Title, UpdatedAt: s.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
