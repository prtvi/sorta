package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/meta"
	"github.com/prtvi/sorta/internal/models"
)

// Server holds HTTP handlers and an in-process undo stack.
type Server struct {
	Store   *filesystem.Store
	storeMu sync.RWMutex

	mu   sync.Mutex
	undo []models.UndoEntry

	libMu    sync.Mutex
	libCache map[models.Bucket]libCacheEntry

	warmMu        sync.Mutex
	warmPersistMu sync.Mutex // serializes meta.json warmDone writes
	warm          thumbWarmState
	warmDone      map[string]meta.WarmDoneEntry
}

type libCacheEntry struct {
	fp        string
	photos    []models.Photo
	bursts    []models.BurstGroup
	singles   []models.Photo
	annotated bool
}

// New creates a Server bound to the given store.
func New(store *filesystem.Store) *Server {
	s := &Server{
		Store:    store,
		libCache: make(map[models.Bucket]libCacheEntry),
		warmDone: make(map[string]meta.WarmDoneEntry),
	}
	s.loadWarmDoneFromDisk()
	return s
}

// Handler returns the root mux for the application.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/config/choose", s.handleChooseConfig)
	mux.HandleFunc("GET /api/photos", s.handleListPhotos)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.HandleFunc("PUT /api/session", s.handlePutSession)
	mux.HandleFunc("GET /api/favorites", s.handleGetFavorites)
	mux.HandleFunc("PUT /api/photos/{filename}/star", s.handlePutStar)
	mux.HandleFunc("GET /api/library/{bucket}", s.handleLibrary)
	mux.HandleFunc("POST /api/library/{bucket}/detect-bursts", s.handleDetectBursts)
	mux.HandleFunc("GET /api/preview/status", s.handlePreviewStatus)
	mux.HandleFunc("POST /api/preview/warmup", s.handlePreviewWarmup)
	mux.HandleFunc("GET /api/photos/{filename}/exif", s.handleExif)
	mux.HandleFunc("POST /api/photos/{filename}/reveal", s.handleReveal)
	mux.HandleFunc("POST /api/photos/{filename}/action", s.handleAction)
	mux.HandleFunc("POST /api/photos/{filename}/burst-keep", s.handleBurstKeep)
	mux.HandleFunc("POST /api/photos/move", s.handleMove)
	mux.HandleFunc("POST /api/photos/move-bulk", s.handleMoveBulk)
	mux.HandleFunc("POST /api/photos/reset-unclassified", s.handleResetUnclassified)
	mux.HandleFunc("POST /api/undo", s.handleUndo)
	mux.HandleFunc("POST /api/export", s.handleExport)
	mux.HandleFunc("GET /photos/{filename}", s.handleServePhoto)
	mux.HandleFunc("GET /photos/{bucket}/{filename}", s.handleServeBucketPhoto)
	return withLogging(s.withRootRequired(mux))
}

// Configured reports whether a working directory has been set.
func (s *Server) Configured() bool {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	return s.Store != nil
}

// SetStore assigns the working-directory store. Returns false if already set.
func (s *Server) SetStore(store *filesystem.Store) bool {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.Store != nil {
		return false
	}
	s.Store = store
	s.loadWarmDoneFromDisk()
	return true
}

func (s *Server) withRootRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api/config" || strings.HasPrefix(path, "/api/config/") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/photos/") {
			if !s.Configured() {
				writeError(w, http.StatusServiceUnavailable, "ROOT_NOT_SET", "Photo directory not configured")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, models.ErrorResponse{
		Success: false,
		Error: models.ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

func (s *Server) pushUndo(records ...models.ActionRecord) {
	if len(records) == 0 {
		return
	}
	s.mu.Lock()
	s.undo = append(s.undo, models.UndoEntry{Records: append([]models.ActionRecord(nil), records...)})
	s.mu.Unlock()
}

func (s *Server) sessionSnapshot() models.SessionStats {
	// Live per-process session counters removed; field kept for API shape.
	return models.SessionStats{}
}

func (s *Server) invalidateLibraryCache() {
	s.libMu.Lock()
	s.libCache = make(map[models.Bucket]libCacheEntry)
	s.libMu.Unlock()
	// Drop only the directory-wide mark so the next status check re-fingerprints.
	// On-disk JPEG caches stay valid (keyed by basename); we must not wipe
	// warm-done and force a full regenerate after every cull move.
	s.invalidateDirectoryWarmMark()
}

func libraryFingerprint(photos []models.Photo) string {
	names := make([]string, len(photos))
	for i, p := range photos {
		names[i] = p.Name
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

