package favorites_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prtvi/sorta/internal/favorites"
	"github.com/prtvi/sorta/internal/models"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	st := models.FavoritesState{Names: []string{"B.jpg", "A.jpg"}}
	if err := favorites.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	got, err := favorites.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Names) != 2 || got.Names[0] != "A.jpg" || got.Names[1] != "B.jpg" {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := favorites.Load(t.TempDir())
	if err != nil || len(got.Names) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	p := favorites.Path(dir)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("{not-json"), 0o644)
	got, err := favorites.Load(dir)
	if err != nil || len(got.Names) != 0 {
		t.Fatalf("corrupt should yield empty: %+v %v", got, err)
	}
}

func TestSetStarred(t *testing.T) {
	dir := t.TempDir()
	st, err := favorites.SetStarred(dir, "A.jpg", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Names) != 1 || st.Names[0] != "A.jpg" {
		t.Fatalf("%+v", st)
	}
	if !favorites.IsStarred(dir, "A.jpg") {
		t.Fatal("expected starred")
	}
	st, err = favorites.SetStarred(dir, "A.jpg", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Names) != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestSaveUsesMetaJSON(t *testing.T) {
	dir := t.TempDir()
	if err := favorites.Save(dir, models.FavoritesState{Names: []string{"A.jpg"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sorta", "meta.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sorta", "favorites.json")); !os.IsNotExist(err) {
		t.Fatal("legacy favorites.json should not be created")
	}
}
