package meta_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/prtvi/sorta/internal/meta"
	"github.com/prtvi/sorta/internal/models"
)

func TestUpdateRead(t *testing.T) {
	dir := t.TempDir()
	err := meta.Update(dir, func(f *meta.File) error {
		f.Session = models.SessionState{Mode: "library", LastPhoto: "A.jpg", LastBucket: "liked"}
		f.Favorites = models.FavoritesState{Names: []string{"A.jpg"}}
		f.WarmDone = map[string]meta.WarmDoneEntry{
			"liked|thumb": {FP: "x", Total: 1},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := meta.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.LastPhoto != "A.jpg" || len(got.Favorites.Names) != 1 || got.WarmDone["liked|thumb"].Total != 1 {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(meta.Path(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLegacyFiles(t *testing.T) {
	dir := t.TempDir()
	sorta := filepath.Join(dir, ".sorta")
	if err := os.MkdirAll(sorta, 0o755); err != nil {
		t.Fatal(err)
	}
	sess, _ := json.Marshal(models.SessionState{Mode: "cull", LastPhoto: "B.jpg"})
	fav, _ := json.Marshal(models.FavoritesState{Names: []string{"B.jpg"}})
	warm, _ := json.Marshal(map[string]meta.WarmDoneEntry{"root|view": {FP: "fp", Total: 2}})
	_ = os.WriteFile(filepath.Join(sorta, "session.json"), sess, 0o644)
	_ = os.WriteFile(filepath.Join(sorta, "favorites.json"), fav, 0o644)
	_ = os.WriteFile(filepath.Join(sorta, "warm-done.json"), warm, 0o644)

	got, err := meta.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.LastPhoto != "B.jpg" {
		t.Fatalf("session migrate: %+v", got.Session)
	}
	if len(got.Favorites.Names) != 1 || got.Favorites.Names[0] != "B.jpg" {
		t.Fatalf("favorites migrate: %+v", got.Favorites)
	}
	if got.WarmDone["root|view"].Total != 2 {
		t.Fatalf("warmDone migrate: %+v", got.WarmDone)
	}
	if _, err := os.Stat(meta.Path(dir)); err != nil {
		t.Fatal("expected meta.json after migrate")
	}
	for _, name := range []string{"session.json", "favorites.json", "warm-done.json"} {
		if _, err := os.Stat(filepath.Join(sorta, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy %s should be removed", name)
		}
	}
}

func TestUpdatePreservesOtherSections(t *testing.T) {
	dir := t.TempDir()
	_ = meta.Update(dir, func(f *meta.File) error {
		f.Session.Mode = "library"
		f.Favorites.Names = []string{"keep.jpg"}
		return nil
	})
	_ = meta.Update(dir, func(f *meta.File) error {
		f.Session.LastPhoto = "next.jpg"
		return nil
	})
	got, err := meta.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.Mode != "library" || got.Session.LastPhoto != "next.jpg" {
		t.Fatalf("session: %+v", got.Session)
	}
	if len(got.Favorites.Names) != 1 || got.Favorites.Names[0] != "keep.jpg" {
		t.Fatalf("favorites clobbered: %+v", got.Favorites)
	}
}

func TestCorruptRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	_ = meta.Update(dir, func(f *meta.File) error {
		f.Session = models.SessionState{Mode: "library", LastPhoto: "keep.jpg"}
		f.Favorites = models.FavoritesState{Names: []string{"keep.jpg"}}
		return nil
	})
	// Destroy primary and backup so restore is impossible.
	_ = os.WriteFile(meta.Path(dir), []byte("{not-json"), 0o644)
	_ = os.Remove(filepath.Join(dir, ".sorta", "meta.json.bak"))

	_, err := meta.Read(dir)
	if !errors.Is(err, meta.ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
	err = meta.Update(dir, func(f *meta.File) error {
		f.Session.LastPhoto = "wiped.jpg"
		return nil
	})
	if !errors.Is(err, meta.ErrCorrupt) {
		t.Fatalf("Update must refuse wipe, got %v", err)
	}
	raw, _ := os.ReadFile(meta.Path(dir))
	if string(raw) != "{not-json" {
		t.Fatalf("disk must stay untouched, got %q", raw)
	}
}

func TestCorruptRestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	_ = meta.Update(dir, func(f *meta.File) error {
		f.Session = models.SessionState{Mode: "cull", LastPhoto: "good.jpg"}
		return nil
	})
	// Force a second write so bak exists with the first good version… actually
	// bak is previous; after one write bak may not exist. Write twice.
	_ = meta.Update(dir, func(f *meta.File) error {
		f.Session.LastPhoto = "good2.jpg"
		return nil
	})
	_ = os.WriteFile(meta.Path(dir), []byte("{broken"), 0o644)

	got, err := meta.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.LastPhoto != "good.jpg" && got.Session.LastPhoto != "good2.jpg" {
		t.Fatalf("expected restore from bak, got %+v", got.Session)
	}
}
