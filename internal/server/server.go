package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/models"
)

// Server holds HTTP handlers, undo stack, and in-process session stats.
type Server struct {
	Store *filesystem.Store
	mu    sync.Mutex
	undo  []models.UndoEntry

	sessionMu     sync.Mutex
	session       models.SessionStats
	decisionSumMs int64
	decisionCount int

	libMu    sync.Mutex
	libCache map[models.Bucket]libCacheEntry
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
	return &Server{
		Store:    store,
		libCache: make(map[models.Bucket]libCacheEntry),
	}
}

// Handler returns the root mux for the application.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/photos", s.handleListPhotos)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.HandleFunc("PUT /api/session", s.handlePutSession)
	mux.HandleFunc("GET /api/library/{bucket}", s.handleLibrary)
	mux.HandleFunc("POST /api/library/{bucket}/detect-bursts", s.handleDetectBursts)
	mux.HandleFunc("GET /api/photos/{filename}/exif", s.handleExif)
	mux.HandleFunc("POST /api/photos/{filename}/action", s.handleAction)
	mux.HandleFunc("POST /api/photos/{filename}/burst-keep", s.handleBurstKeep)
	mux.HandleFunc("POST /api/photos/move", s.handleMove)
	mux.HandleFunc("POST /api/photos/move-bulk", s.handleMoveBulk)
	mux.HandleFunc("POST /api/photos/reset-unclassified", s.handleResetUnclassified)
	mux.HandleFunc("POST /api/undo", s.handleUndo)
	mux.HandleFunc("POST /api/export/liked", s.handleExportLiked)
	mux.HandleFunc("GET /photos/{filename}", s.handleServePhoto)
	mux.HandleFunc("GET /photos/{bucket}/{filename}", s.handleServeBucketPhoto)
	return withLogging(mux)
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

func (s *Server) recordSessionMove(to models.Bucket, decisionMs int64) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.session.Reviewed++
	switch to {
	case models.BucketLiked:
		s.session.Liked++
	case models.BucketDisliked:
		s.session.Disliked++
	case models.BucketReview:
		s.session.Review++
	}
	if decisionMs > 0 {
		s.decisionSumMs += decisionMs
		s.decisionCount++
		s.session.AvgDecisionSeconds = float64(s.decisionSumMs) / float64(s.decisionCount) / 1000.0
	}
}

func (s *Server) sessionSnapshot() models.SessionStats {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	return s.session
}

func (s *Server) invalidateLibraryCache() {
	s.libMu.Lock()
	s.libCache = make(map[models.Bucket]libCacheEntry)
	s.libMu.Unlock()
}

func libraryFingerprint(photos []models.Photo) string {
	names := make([]string, len(photos))
	for i, p := range photos {
		names[i] = p.Name
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

// DecisionHeader is an optional client header with decision duration in ms.
const DecisionHeader = "X-Decision-Ms"

func decisionMsFrom(r *http.Request) int64 {
	v := r.Header.Get(DecisionHeader)
	if v == "" {
		return 0
	}
	var ms int64
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0
		}
		ms = ms*10 + int64(c-'0')
	}
	if ms > int64((time.Hour / time.Millisecond)) {
		return 0
	}
	return ms
}
