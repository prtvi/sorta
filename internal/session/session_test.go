package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/session"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	st := models.SessionState{Mode: "library", LastPhoto: "A.jpg", LastBucket: "liked"}
	if err := session.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	got, err := session.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != st {
		t.Fatalf("%+v != %+v", got, st)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := session.Load(t.TempDir())
	if err != nil || got.LastPhoto != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	p := session.Path(dir)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("{not-json"), 0o644)
	got, err := session.Load(dir)
	if err != nil || got.Mode != "" {
		t.Fatalf("corrupt should yield empty: %+v %v", got, err)
	}
	// Save must not wipe the corrupt file with empty session.
	if err := session.Save(dir, models.SessionState{Mode: "cull"}); err == nil {
		t.Fatal("Save should fail when meta is corrupt with no backup")
	}
	raw, _ := os.ReadFile(p)
	if string(raw) != "{not-json" {
		t.Fatalf("corrupt file must not be overwritten, got %q", raw)
	}
}

func TestSaveUsesMetaJSON(t *testing.T) {
	dir := t.TempDir()
	if err := session.Save(dir, models.SessionState{Mode: "cull"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sorta", "meta.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sorta", "session.json")); !os.IsNotExist(err) {
		t.Fatal("legacy session.json should not be created")
	}
}
