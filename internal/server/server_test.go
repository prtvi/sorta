package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/server"
)

func setup(t *testing.T) (*server.Server, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := filesystem.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureClassificationDirs(); err != nil {
		t.Fatal(err)
	}
	// Minimal valid JPEG (1x1) so preview generation works in tests.
	tinyJPEG := []byte{
		0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01,
		0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43,
		0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09,
		0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12,
		0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20,
		0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29,
		0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27, 0x39, 0x3d, 0x38, 0x32,
		0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01,
		0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x14, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x08, 0xff, 0xc4, 0x00, 0x14, 0x10, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00,
		0x7f, 0xbf, 0xff, 0xd9,
	}
	tinyPNG := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	for _, name := range []string{"A.jpg", "B.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), tinyJPEG, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "C.png"), tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	return server.New(store), dir
}

func TestListPhotos(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var resp models.PhotosResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Photos) != 3 {
		t.Fatalf("got %d photos", len(resp.Photos))
	}
	if resp.Stats.Remaining != 3 || resp.Stats.Total != 3 {
		t.Fatalf("stats %+v", resp.Stats)
	}
	for _, p := range resp.Photos {
		if p.BurstID != "" || p.BurstSize != 0 {
			t.Fatalf("cull list must not annotate bursts: %+v", p)
		}
	}
}

func TestLibraryRootGroups(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/api/library/root", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var lib models.LibraryResponse
	if err := json.NewDecoder(rr.Body).Decode(&lib); err != nil {
		t.Fatal(err)
	}
	if lib.Bucket != "root" || len(lib.Photos) != 3 {
		t.Fatalf("%+v", lib)
	}
	if len(lib.Bursts) != 0 {
		t.Fatalf("library list must not annotate bursts: %+v", lib.Bursts)
	}
	if len(lib.Singles) != len(lib.Photos) {
		t.Fatalf("singles=%d photos=%d", len(lib.Singles), len(lib.Photos))
	}
	for _, p := range lib.Photos {
		if p.BurstID != "" || p.BurstSize != 0 {
			t.Fatalf("unexpected burst annotation: %+v", p)
		}
	}
}

func TestServePhoto(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/photos/A.jpg", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("content-type %q", ct)
	}
	body, _ := io.ReadAll(rr.Body)
	if len(body) < 16 {
		t.Fatalf("expected jpeg bytes, got %d", len(body))
	}
}

func TestServePhotoThumb(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/photos/A.jpg?size=thumb", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("content-type %q", ct)
	}
}

func TestPreviewWarmup(t *testing.T) {
	srv, _ := setup(t)

	req := httptest.NewRequest(http.MethodGet, "/api/preview/status?bucket=root", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var before models.PreviewWarmupStatus
	if err := json.NewDecoder(rr.Body).Decode(&before); err != nil {
		t.Fatal(err)
	}
	if before.Total != 3 || before.Pending != 3 {
		t.Fatalf("before warmup: %+v", before)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/preview/warmup?bucket=root", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("warmup status %d %s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(15 * time.Second)
	var last models.PreviewWarmupStatus
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/api/preview/status?bucket=root", nil)
		rr = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("poll status %d", rr.Code)
		}
		last = models.PreviewWarmupStatus{}
		if err := json.NewDecoder(rr.Body).Decode(&last); err != nil {
			t.Fatal(err)
		}
		if !last.Running && last.Pending == 0 && last.Ready+last.Failed >= last.Total {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last.Pending != 0 || last.Ready < 1 {
		t.Fatalf("warmup did not finish: %+v", last)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/preview/warmup?bucket=root", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var again models.PreviewWarmupStatus
	if err := json.NewDecoder(rr.Body).Decode(&again); err != nil {
		t.Fatal(err)
	}
	if again.Running || again.Pending != 0 {
		t.Fatalf("second warmup should be a no-op: %+v", again)
	}
}

func TestServePhotoPathTraversal(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/photos/..%2F..%2Fetc%2Fpasswd", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatal("traversal should not succeed")
	}
}

func TestRevealPhoto(t *testing.T) {
	srv, dir := setup(t)
	prev := server.RevealHook
	var got string
	server.RevealHook = func(path string) error {
		got = path
		return nil
	}
	t.Cleanup(func() { server.RevealHook = prev })

	req := httptest.NewRequest(http.MethodPost, "/api/photos/A.jpg/reveal?from=root", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	want := filepath.Join(dir, "A.jpg")
	if got != want {
		t.Fatalf("revealed %q want %q", got, want)
	}
	var resp models.RevealResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Path != want {
		t.Fatalf("resp %+v", resp)
	}
}

func TestActionAndUndo(t *testing.T) {
	srv, dir := setup(t)

	body := bytes.NewBufferString(`{"action":"liked"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/photos/A.jpg/action", body)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "A.jpg")); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/undo", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var undo models.UndoResponse
	if err := json.NewDecoder(rr.Body).Decode(&undo); err != nil {
		t.Fatal(err)
	}
	if !undo.Success || undo.Photo != "A.jpg" {
		t.Fatalf("undo %+v", undo)
	}
	if _, err := os.Stat(filepath.Join(dir, "A.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestUndoEmpty(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodPost, "/api/undo", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var undo models.UndoResponse
	_ = json.NewDecoder(rr.Body).Decode(&undo)
	if undo.Success || undo.Reason != "nothing_to_undo" {
		t.Fatalf("%+v", undo)
	}
}

func TestInvalidAction(t *testing.T) {
	srv, _ := setup(t)
	body := bytes.NewBufferString(`{"action":"delete"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/photos/A.jpg/action", body)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestMissingPhotoAction(t *testing.T) {
	srv, _ := setup(t)
	body := bytes.NewBufferString(`{"action":"sharing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/photos/nope.jpg/action", body)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestExifEndpoint(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/api/photos/A.jpg/exif", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var meta models.ExifMetadata
	if err := json.NewDecoder(rr.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryAndMove(t *testing.T) {
	srv, dir := setup(t)
	body := bytes.NewBufferString(`{"action":"liked"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/photos/A.jpg/action", body)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	req = httptest.NewRequest(http.MethodGet, "/api/library/liked", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var lib models.LibraryResponse
	_ = json.NewDecoder(rr.Body).Decode(&lib)
	if lib.Bucket != "liked" || len(lib.Photos) != 1 {
		t.Fatalf("%+v", lib)
	}

	body = bytes.NewBufferString(`{"filename":"A.jpg","from":"liked","to":"sharing"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/photos/move", body)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s", rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "sharing", "A.jpg")); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/photos/sharing/A.jpg", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("serve bucket %d", rr.Code)
	}
}

func TestBulkMoveAndUndo(t *testing.T) {
	srv, dir := setup(t)
	for _, name := range []string{"A.jpg", "B.jpg"} {
		body := bytes.NewBufferString(`{"action":"liked"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/photos/"+name+"/action", body)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
	}

	payload := `{"photos":[{"filename":"A.jpg","from":"liked"},{"filename":"B.jpg","from":"liked"}],"to":"disliked"}`
	req := httptest.NewRequest(http.MethodPost, "/api/photos/move-bulk", bytes.NewBufferString(payload))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var bulk models.BulkMoveResponse
	_ = json.NewDecoder(rr.Body).Decode(&bulk)
	if !bulk.Success || bulk.MovedCount != 2 {
		t.Fatalf("%+v", bulk)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/undo", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var undo models.UndoResponse
	_ = json.NewDecoder(rr.Body).Decode(&undo)
	if !undo.Success || undo.Count != 2 {
		t.Fatalf("%+v", undo)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "A.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked", "B.jpg")); err != nil {
		t.Fatal(err)
	}
}

func TestResetUnclassified(t *testing.T) {
	srv, dir := setup(t)
	for _, name := range []string{"A.jpg", "B.jpg"} {
		body := bytes.NewBufferString(`{"action":"liked"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/photos/"+name+"/action", body)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
	}
	body := bytes.NewBufferString(`{"action":"disliked"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/photos/C.png/action", body)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	req = httptest.NewRequest(http.MethodPost, "/api/photos/reset-unclassified", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s", rr.Body.String())
	}
	var res models.ResetUnclassifiedResponse
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if !res.Success || res.MovedCount != 3 {
		t.Fatalf("%+v", res)
	}
	for _, name := range []string{"A.jpg", "B.jpg", "C.png"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStatsShape(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var resp models.StatsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Filesystem.Root != 3 {
		t.Fatalf("%+v", resp)
	}
}

func TestInvalidLibraryBucket(t *testing.T) {
	srv, _ := setup(t)
	req := httptest.NewRequest(http.MethodGet, "/api/library/nope", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestFavoritesStar(t *testing.T) {
	srv, dir := setup(t)

	req := httptest.NewRequest(http.MethodGet, "/api/favorites", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var empty models.FavoritesState
	if err := json.NewDecoder(rr.Body).Decode(&empty); err != nil {
		t.Fatal(err)
	}
	if len(empty.Names) != 0 {
		t.Fatalf("%+v", empty)
	}

	body := bytes.NewBufferString(`{"starred":true}`)
	req = httptest.NewRequest(http.MethodPut, "/api/photos/A.jpg/star", body)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var star models.StarResponse
	if err := json.NewDecoder(rr.Body).Decode(&star); err != nil {
		t.Fatal(err)
	}
	if !star.Success || !star.Starred || star.Filename != "A.jpg" {
		t.Fatalf("%+v", star)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/favorites", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var fav models.FavoritesState
	_ = json.NewDecoder(rr.Body).Decode(&fav)
	if len(fav.Names) != 1 || fav.Names[0] != "A.jpg" {
		t.Fatalf("%+v", fav)
	}
	if _, err := os.Stat(filepath.Join(dir, ".sorta", "favorites.json")); err != nil {
		t.Fatal(err)
	}

	body = bytes.NewBufferString(`{"starred":false}`)
	req = httptest.NewRequest(http.MethodPut, "/api/photos/A.jpg/star", body)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unstar %d", rr.Code)
	}

	body = bytes.NewBufferString(`{"starred":true}`)
	req = httptest.NewRequest(http.MethodPut, "/api/photos/missing.jpg/star", body)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing photo status %d", rr.Code)
	}
}

func TestConfigDeferredRoot(t *testing.T) {
	srv := server.New(nil)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get config %d", rr.Code)
	}
	var cfg models.ConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Configured {
		t.Fatal("expected unconfigured")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/photos", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("photos without root: %d", rr.Code)
	}

	dir := t.TempDir()
	body := bytes.NewBufferString(`{"path":` + jsonQuote(dir) + `}`)
	req = httptest.NewRequest(http.MethodPut, "/api/config", body)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put config %d: %s", rr.Code, rr.Body.String())
	}
	if err := json.NewDecoder(rr.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Configured || cfg.Root == "" {
		t.Fatalf("expected configured root, got %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, "liked")); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/photos", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("photos after config %d", rr.Code)
	}

	body = bytes.NewBufferString(`{"path":` + jsonQuote(t.TempDir()) + `}`)
	req = httptest.NewRequest(http.MethodPut, "/api/config", body)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("reconfigure status %d", rr.Code)
	}
}

func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
