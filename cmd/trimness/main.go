package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"trimness/internal/agent"
	"trimness/internal/llm"
	"trimness/internal/session"
	"trimness/internal/tools"
	"trimness/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
)

func main() {
	sessionID := flag.String("session", "", "resume the session with this ID (default: most recent)")
	flag.Parse()

	// A missing .env is fine; the key may come from the real environment.
	_ = godotenv.Load()

	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		log.Fatal("GEMINI_API_KEY is not set (export it or put it in .env)")
	}

	dir, err := session.DefaultDir()
	if err != nil {
		log.Fatalf("cannot resolve session directory: %v", err)
	}
	store := session.FileStore{Dir: dir}

	sess, err := initialSession(store, *sessionID)
	if err != nil {
		log.Fatal(err)
	}

	// Tools are confined to the directory trimness is started in: os.Root
	// refuses paths that escape it (.., absolute paths, symlinks out).
	root, err := os.OpenRoot(".")
	if err != nil {
		log.Fatalf("cannot open project directory: %v", err)
	}
	defer root.Close()

	a := &agent.Agent{
		Name:   "Trimness",
		Client: llm.GeminiClient{API_key: key, Model: "gemini-3.5-flash"},
		Tools:  tools.NewRegistry(tools.ReadFile{Root: root}, tools.WriteFile{Root: root}),
	}

	p := tea.NewProgram(tui.New(a, store, sess), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// initialSession resolves which session to open: an explicit ID, else the most
// recently updated one, else a brand-new session.
func initialSession(store session.Store, id string) (session.Session, error) {
	if id != "" {
		s, err := store.Load(id)
		if err != nil {
			return session.Session{}, fmt.Errorf("cannot open session %q: %w", id, err)
		}
		return s, nil
	}
	sums, err := store.List()
	if err != nil {
		return session.Session{}, fmt.Errorf("cannot list sessions: %w", err)
	}
	if len(sums) > 0 {
		if s, err := store.Load(sums[0].ID); err == nil {
			return s, nil
		}
	}
	return session.New(), nil
}
