package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/models"
)

func TestNewStoreAndEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureClassificationDirs(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"liked", "disliked", "review", "deleted"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.IsDir() {
			t.Fatalf("missing dir %s: %v", name, err)
		}
	}
}

func TestMoveToDeletedTakesARW(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	if err := os.WriteFile(filepath.Join(dir, "liked", "gone.jpg"), []byte("j"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liked", "gone.ARW"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := store.MoveBetweenBuckets("gone.jpg", models.BucketLiked, models.BucketDeleted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deleted", "gone.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deleted", "gone.ARW")); err != nil {
		t.Fatalf("ARW should move to deleted: %v", err)
	}
	if _, err := store.UndoMove(*rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "gone.ARW")); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeFilename(t *testing.T) {
	if _, err := filesystem.SanitizeFilename("../etc/passwd"); err == nil {
		t.Fatal("expected error for traversal")
	}
	if _, err := filesystem.SanitizeFilename("a/b.jpg"); err == nil {
		t.Fatal("expected error for slash")
	}
	got, err := filesystem.SanitizeFilename("DSC0001.JPG")
	if err != nil || got != "DSC0001.JPG" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestMoveAndUndo(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureClassificationDirs(); err != nil {
		t.Fatal(err)
	}
	name := "DSC0001.JPG"
	src := filepath.Join(dir, name)
	if err := os.WriteFile(src, []byte("photo"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := store.MoveToClassification(name, models.ActionLiked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should be gone")
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", name)); err != nil {
		t.Fatal(err)
	}

	restored, err := store.UndoMove(*rec)
	if err != nil {
		t.Fatal(err)
	}
	if restored != name {
		t.Fatalf("restored name %q", restored)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal(err)
	}
}

func TestMoveBetweenBuckets(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	if err := os.WriteFile(filepath.Join(dir, "liked", "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := store.MoveBetweenBuckets("a.jpg", models.BucketLiked, models.BucketReview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "review", "a.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UndoMove(*rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "a.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestMoveToRoot(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	if err := os.WriteFile(filepath.Join(dir, "disliked", "b.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MoveBetweenBuckets("b.jpg", models.BucketDisliked, models.BucketRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestMoveConflictUsesUniqueName(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	if err := os.WriteFile(filepath.Join(dir, "liked", "a.jpg"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := store.MoveToClassification("a.jpg", models.ActionLiked)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Filename != "a (1).jpg" {
		t.Fatalf("expected unique name, got %q", rec.Filename)
	}
	data, err := os.ReadFile(filepath.Join(dir, "liked", "a.jpg"))
	if err != nil || string(data) != "old" {
		t.Fatalf("overwrote existing file")
	}
}

func TestMoveMissingFile(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	_, err = store.MoveToClassification("missing.jpg", models.ActionDisliked)
	if !os.IsNotExist(err) {
		t.Fatalf("expected not exist, got %v", err)
	}
}

func TestResolveRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveInRoot(".."); err == nil {
		t.Fatal("expected error")
	}
	if _, err := store.ResolveInRoot("../../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMultipleUndoOrder(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()

	actions := []struct {
		name   string
		action models.PhotoAction
	}{
		{"A.jpg", models.ActionLiked},
		{"B.jpg", models.ActionDisliked},
		{"C.jpg", models.ActionReview},
	}
	var stack []models.ActionRecord
	for _, a := range actions {
		if err := os.WriteFile(filepath.Join(dir, a.name), []byte(a.name), 0o644); err != nil {
			t.Fatal(err)
		}
		rec, err := store.MoveToClassification(a.name, a.action)
		if err != nil {
			t.Fatal(err)
		}
		stack = append(stack, *rec)
	}

	for i := len(stack) - 1; i >= 0; i-- {
		name, err := store.UndoMove(stack[i])
		if err != nil {
			t.Fatal(err)
		}
		if name != actions[i].name {
			t.Fatalf("undo got %q want %q", name, actions[i].name)
		}
		if _, err := os.Stat(filepath.Join(dir, actions[i].name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMoveTakesARWCompanion(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()

	jpg := "DSC0001.JPG"
	arw := "DSC0001.ARW"
	if err := os.WriteFile(filepath.Join(dir, jpg), []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, arw), []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := store.MoveToClassification(jpg, models.ActionLiked)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Companions) != 1 {
		t.Fatalf("expected 1 companion, got %d", len(rec.Companions))
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", jpg)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", arw)); err != nil {
		t.Fatalf("ARW should have moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, arw)); !os.IsNotExist(err) {
		t.Fatal("ARW should be gone from root")
	}

	if _, err := store.UndoMove(*rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, jpg)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, arw)); err != nil {
		t.Fatalf("ARW should be restored: %v", err)
	}
}

func TestMoveARWCompanionUsesConflictStem(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()

	if err := os.WriteFile(filepath.Join(dir, "liked", "a.jpg"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ARW"), []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := store.MoveToClassification("a.jpg", models.ActionLiked)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Filename != "a (1).jpg" {
		t.Fatalf("expected unique jpg name, got %q", rec.Filename)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "a (1).ARW")); err != nil {
		t.Fatalf("ARW should share conflict stem: %v", err)
	}
}

func TestMoveBetweenBucketsTakesARW(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	if err := os.WriteFile(filepath.Join(dir, "liked", "shot.jpg"), []byte("j"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liked", "shot.arw"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := store.MoveBetweenBuckets("shot.jpg", models.BucketLiked, models.BucketReview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "review", "shot.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "review", "shot.arw")); err != nil {
		t.Fatalf("lowercase .arw should move: %v", err)
	}
	if _, err := store.UndoMove(*rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "shot.arw")); err != nil {
		t.Fatal(err)
	}
}

func TestMoveWithoutARWStillWorks(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	if err := os.WriteFile(filepath.Join(dir, "solo.jpg"), []byte("j"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := store.MoveToClassification("solo.jpg", models.ActionDisliked)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Companions) != 0 {
		t.Fatalf("unexpected companions: %+v", rec.Companions)
	}
}

func TestListLibraryBucket(t *testing.T) {
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.EnsureClassificationDirs()
	_ = os.WriteFile(filepath.Join(dir, "liked", "z.jpg"), []byte("z"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "liked", "a.jpg"), []byte("a"), 0o644)
	photos, err := store.ListLibraryBucket(models.BucketLiked)
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != 2 || photos[0].Name != "a.jpg" {
		t.Fatalf("%+v", photos)
	}
	if photos[0].URL != "/photos/liked/a.jpg" {
		t.Fatalf("url %s", photos[0].URL)
	}
}
