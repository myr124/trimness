package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"trimness/internal/core"
)

func TestRoundTrip(t *testing.T) {
	fs := FileStore{Dir: filepath.Join(t.TempDir(), "sessions")}
	s := New()
	s.Messages = []core.Message{{Role: "user", Content: "hello there"}, {Role: "assistant", Content: "hi"}}
	s.DeriveTitle()
	if err := fs.Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "hello there" || len(got.Messages) != 2 || got.Messages[1].Content != "hi" {
		t.Fatalf("unexpected session: %+v", got)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	fs := FileStore{Dir: dir}
	s := New()
	for i := 0; i < 3; i++ {
		if err := fs.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
}

func TestListOrderAndCorruptSkipped(t *testing.T) {
	dir := t.TempDir()
	fs := FileStore{Dir: dir}
	old, recent := New(), New()
	old.UpdatedAt = time.Now().Add(-time.Hour)
	recent.UpdatedAt = time.Now()
	for _, s := range []Session{old, recent} {
		if err := fs.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := fs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != recent.ID {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestListMissingDir(t *testing.T) {
	list, err := FileStore{Dir: filepath.Join(t.TempDir(), "nope")}.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("got %v, %v", list, err)
	}
}

func TestInvalidID(t *testing.T) {
	fs := FileStore{Dir: t.TempDir()}
	for _, id := range []string{"", "../x", "a/b", ".hidden"} {
		if _, err := fs.Load(id); err == nil {
			t.Errorf("expected error for id %q", id)
		}
	}
}

func TestDeriveTitleTruncates(t *testing.T) {
	s := New()
	s.Messages = []core.Message{{Role: "user", Content: "a very long prompt that definitely exceeds the forty character limit"}}
	s.DeriveTitle()
	if len([]rune(s.Title)) != titleMax+1 {
		t.Fatalf("title %q", s.Title)
	}
}
