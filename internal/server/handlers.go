package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/prtvi/sorta/internal/burst"
	"github.com/prtvi/sorta/internal/exifmeta"
	exportpkg "github.com/prtvi/sorta/internal/export"
	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/preview"
	"github.com/prtvi/sorta/internal/scanner"
	"github.com/prtvi/sorta/internal/session"
)

func (s *Server) handleListPhotos(w http.ResponseWriter, r *http.Request) {
	// Cull mode: plain scan only — burst detection lives in Library.
	photos, err := scanner.ScanRoot(s.Store.Root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan photos")
		return
	}
	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}
	writeJSON(w, http.StatusOK, models.PhotosResponse{
		Photos: photos,
		Stats:  stats,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}
	writeJSON(w, http.StatusOK, models.StatsResponse{
		Filesystem: stats,
		Session:    s.sessionSnapshot(),
	})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	st, _ := session.Load(s.Store.Root)
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePutSession(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body")
		return
	}
	var st models.SessionState
	if err := json.Unmarshal(body, &st); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}
	if err := session.Save(s.Store.Root, st); err != nil {
		writeError(w, http.StatusInternalServerError, "SESSION_SAVE_FAILED", "Could not save session")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	bucket := models.Bucket(r.PathValue("bucket"))
	if !bucket.LibraryBucket() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid library bucket")
		return
	}
	photos, err := s.Store.ListLibraryBucket(bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan library bucket")
		return
	}
	fp := libraryFingerprint(photos)

	bursts := []models.BurstGroup{}
	singles := photos

	s.libMu.Lock()
	cached, hit := s.libCache[bucket]
	s.libMu.Unlock()
	if hit && cached.fp == fp && cached.annotated {
		photos = cached.photos
		bursts = cached.bursts
		singles = cached.singles
		if bursts == nil {
			bursts = []models.BurstGroup{}
		}
		if singles == nil {
			singles = []models.Photo{}
		}
	} else if !(hit && cached.fp == fp) {
		s.libMu.Lock()
		s.libCache[bucket] = libCacheEntry{fp: fp, photos: photos, bursts: nil, singles: photos, annotated: false}
		s.libMu.Unlock()
	}

	var statsPtr *models.Stats
	if stats, err := s.Store.CollectStats(); err == nil {
		statsPtr = &stats
	}

	writeJSON(w, http.StatusOK, models.LibraryResponse{
		Bucket:  bucket,
		Photos:  photos,
		Bursts:  bursts,
		Singles: singles,
		Stats:   statsPtr,
	})
}

func (s *Server) handleDetectBursts(w http.ResponseWriter, r *http.Request) {
	bucket := models.Bucket(r.PathValue("bucket"))
	if !bucket.LibraryBucket() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid library bucket")
		return
	}
	photos, err := s.Store.ListLibraryBucket(bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan library bucket")
		return
	}
	dir, err := s.Store.BucketDir(bucket)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid library bucket")
		return
	}

	fp := libraryFingerprint(photos)
	photos = burst.AnnotateDir(dir, photos, burst.DefaultMaxGap, burst.DefaultMinSize)
	bursts, singles := burst.GroupBursts(photos)

	s.libMu.Lock()
	s.libCache[bucket] = libCacheEntry{
		fp:        fp,
		photos:    photos,
		bursts:    bursts,
		singles:   singles,
		annotated: true,
	}
	s.libMu.Unlock()

	var statsPtr *models.Stats
	if stats, err := s.Store.CollectStats(); err == nil {
		statsPtr = &stats
	}

	writeJSON(w, http.StatusOK, models.LibraryResponse{
		Bucket:  bucket,
		Photos:  photos,
		Bursts:  bursts,
		Singles: singles,
		Stats:   statsPtr,
	})
}

func (s *Server) handleExif(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	var path string
	var err error
	if from := models.Bucket(r.URL.Query().Get("from")); from.LibraryBucket() {
		path, err = s.Store.ResolveInBucket(from, filename)
	} else {
		path, err = s.Store.ResolveInRoot(filename)
		if err == nil {
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				path, err = s.Store.FindInLibrary(filename)
			}
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "FILE_READ_FAILED", "Could not read photo")
		return
	}

	meta, err := exifmeta.Read(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EXIF_READ_FAILED", "Could not read EXIF")
		return
	}
	writeJSON(w, http.StatusOK, models.ExifMetadata{
		Camera:      meta.Camera,
		Lens:        meta.Lens,
		FocalLength: meta.FocalLength,
		Aperture:    meta.Aperture,
		Shutter:     meta.Shutter,
		ISO:         meta.ISO,
		TakenAt:     meta.TakenAt,
		HasGPS:      meta.HasGPS,
	})
}

func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, path, filename string) {
	size := preview.ParseSize(r.URL.Query().Get("size"))
	jpegPath, err := preview.DisplayJPEG(s.Store.Root, path, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PREVIEW_FAILED", "Could not generate image preview")
		return
	}

	f, err := os.Open(jpegPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "FILE_READ_FAILED", "Could not read photo")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "FILE_READ_FAILED", "Could not read photo")
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}

	serveName := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".jpg"
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, serveName, info.ModTime(), f)
}

func (s *Server) handleServePhoto(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	path, err := s.Store.ResolveInRoot(filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}
	s.serveImage(w, r, path, filename)
}

func (s *Server) handleServeBucketPhoto(w http.ResponseWriter, r *http.Request) {
	bucket := models.Bucket(r.PathValue("bucket"))
	filename := r.PathValue("filename")
	if !bucket.LibraryBucket() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid bucket")
		return
	}
	path, err := s.Store.ResolveInBucket(bucket, filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}
	s.serveImage(w, r, path, filename)
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if _, err := filesystem.SanitizeFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body")
		return
	}
	var req models.ActionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}
	if !req.Action.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_ACTION", "Action must be liked, disliked, or review")
		return
	}

	rec, err := s.Store.MoveToClassification(filename, req.Action)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found")
			return
		}
		if strings.Contains(err.Error(), "invalid") {
			writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
			return
		}
		writeError(w, http.StatusInternalServerError, "FILE_MOVE_FAILED", "Could not move photo")
		return
	}

	s.pushUndo(*rec)
	s.recordSessionMove(req.Action.ToBucket(), decisionMsFrom(r))
	s.invalidateLibraryCache()

	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}

	writeJSON(w, http.StatusOK, models.ActionResponse{
		Success: true,
		Photo:   filename,
		Action:  req.Action,
		Stats:   stats,
	})
}

func (s *Server) handleBurstKeep(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if _, err := filesystem.SanitizeFilename(filename); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body")
		return
	}
	var req models.BurstKeepRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}
	if req.KeepAction == "" {
		req.KeepAction = models.ActionLiked
	}
	if req.RestAction == "" {
		req.RestAction = models.ActionDisliked
	}
	if req.From == "" {
		req.From = models.BucketRoot
	}
	if !req.KeepAction.Valid() || !req.RestAction.Valid() || !req.From.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_ACTION", "Invalid keep_action, rest_action, or from")
		return
	}

	photos, err := s.Store.ListLibraryBucket(req.From)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan photos")
		return
	}
	dir, err := s.Store.BucketDir(req.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid from bucket")
		return
	}
	photos = burst.AnnotateDir(dir, photos, burst.DefaultMaxGap, burst.DefaultMinSize)

	var keepPhoto *models.Photo
	for i := range photos {
		if photos[i].Name == filename {
			keepPhoto = &photos[i]
			break
		}
	}
	if keepPhoto == nil {
		writeError(w, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found")
		return
	}
	if keepPhoto.BurstID == "" || keepPhoto.BurstSize < 2 {
		writeError(w, http.StatusBadRequest, "NOT_IN_BURST", "Photo is not part of a burst")
		return
	}

	members := burst.Members(photos, keepPhoto.BurstID)
	moved := make([]string, 0, len(members))
	var records []models.ActionRecord

	for _, name := range members {
		to := req.RestAction.ToBucket()
		if name == filename {
			to = req.KeepAction.ToBucket()
		}
		if req.From == to {
			continue
		}
		rec, err := s.Store.MoveBetweenBuckets(name, req.From, to)
		if err != nil {
			if len(records) > 0 {
				s.pushUndo(records...)
			}
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found during burst keep")
				return
			}
			writeError(w, http.StatusInternalServerError, "FILE_MOVE_FAILED", "Could not complete burst keep")
			return
		}
		records = append(records, *rec)
		moved = append(moved, name)
		s.recordSessionMove(to, 0)
	}

	s.pushUndo(records...)
	s.invalidateLibraryCache()

	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}

	writeJSON(w, http.StatusOK, models.BurstKeepResponse{
		Success: true,
		Kept:    filename,
		Moved:   moved,
		Stats:   stats,
	})
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body")
		return
	}
	var req models.MoveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}
	if _, err := filesystem.SanitizeFilename(req.Filename); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILENAME", "Invalid filename")
		return
	}
	if !req.From.Valid() || !req.To.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid from/to bucket")
		return
	}

	rec, err := s.Store.MoveBetweenBuckets(req.Filename, req.From, req.To)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found")
			return
		}
		if strings.Contains(err.Error(), "same") {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "FILE_MOVE_FAILED", "Could not move photo")
		return
	}

	s.pushUndo(*rec)
	s.recordSessionMove(req.To, decisionMsFrom(r))
	s.invalidateLibraryCache()

	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}

	writeJSON(w, http.StatusOK, models.MoveResponse{
		Success:  true,
		Filename: req.Filename,
		From:     req.From,
		To:       req.To,
		Stats:    stats,
	})
}

func (s *Server) handleMoveBulk(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body")
		return
	}
	var req models.BulkMoveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}
	if !req.To.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid destination bucket")
		return
	}
	if len(req.Photos) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "No photos provided")
		return
	}

	results := make([]models.BulkMoveItemResult, 0, len(req.Photos))
	var records []models.ActionRecord
	moved, failed := 0, 0

	for _, item := range req.Photos {
		res := models.BulkMoveItemResult{Filename: item.Filename}
		if _, err := filesystem.SanitizeFilename(item.Filename); err != nil {
			res.Error = "INVALID_FILENAME"
			failed++
			results = append(results, res)
			continue
		}
		if !item.From.Valid() {
			res.Error = "INVALID_BUCKET"
			failed++
			results = append(results, res)
			continue
		}
		if item.From == req.To {
			res.Error = "INVALID_REQUEST"
			failed++
			results = append(results, res)
			continue
		}
		rec, err := s.Store.MoveBetweenBuckets(item.Filename, item.From, req.To)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				res.Error = "PHOTO_NOT_FOUND"
			} else {
				res.Error = "FILE_MOVE_FAILED"
			}
			failed++
			results = append(results, res)
			continue
		}
		res.Success = true
		moved++
		records = append(records, *rec)
		s.recordSessionMove(req.To, 0)
		results = append(results, res)
	}

	s.pushUndo(records...)
	s.invalidateLibraryCache()

	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}

	writeJSON(w, http.StatusOK, models.BulkMoveResponse{
		Success:     failed == 0,
		MovedCount:  moved,
		FailedCount: failed,
		Results:     results,
		Stats:       stats,
	})
}

func (s *Server) handleResetUnclassified(w http.ResponseWriter, r *http.Request) {
	sources := []models.Bucket{models.BucketLiked, models.BucketReview, models.BucketDisliked}
	var records []models.ActionRecord
	moved, failed := 0, 0

	for _, from := range sources {
		photos, err := s.Store.ListLibraryBucket(from)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan library bucket")
			return
		}
		for _, p := range photos {
			rec, err := s.Store.MoveBetweenBuckets(p.Name, from, models.BucketRoot)
			if err != nil {
				failed++
				continue
			}
			records = append(records, *rec)
			moved++
		}
	}

	s.pushUndo(records...)
	s.invalidateLibraryCache()

	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}

	writeJSON(w, http.StatusOK, models.ResetUnclassifiedResponse{
		Success:     failed == 0,
		MovedCount:  moved,
		FailedCount: failed,
		Stats:       stats,
	})
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if len(s.undo) == 0 {
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, models.UndoResponse{
			Success: false,
			Reason:  "nothing_to_undo",
		})
		return
	}
	entry := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	s.mu.Unlock()

	restored := make([]models.UndoPhoto, 0, len(entry.Records))
	var lastName string
	for i := len(entry.Records) - 1; i >= 0; i-- {
		rec := entry.Records[i]
		name, err := s.Store.UndoMove(rec)
		if err != nil {
			remaining := entry.Records[:i+1]
			s.mu.Lock()
			s.undo = append(s.undo, models.UndoEntry{Records: remaining})
			s.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "FILE_MOVE_FAILED", "Could not undo move")
			return
		}
		lastName = name
		bucket := rec.FromBucket
		if bucket == "" {
			bucket = models.BucketRoot
		}
		url := "/photos/" + name
		if bucket.LibraryBucket() {
			url = "/photos/" + string(bucket) + "/" + name
		}
		restored = append(restored, models.UndoPhoto{
			Name:   name,
			URL:    url,
			Bucket: bucket,
		})
	}

	s.invalidateLibraryCache()

	stats, err := s.Store.CollectStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STATS_FAILED", "Could not collect stats")
		return
	}

	writeJSON(w, http.StatusOK, models.UndoResponse{
		Success: true,
		Photo:   lastName,
		Photos:  restored,
		Count:   len(entry.Records),
		Stats:   stats,
	})
}

func (s *Server) handleExportLiked(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body")
		return
	}
	var req models.ExportLikedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Destination) == "" {
		writeError(w, http.StatusBadRequest, "INVALID_DESTINATION", "Destination path is required")
		return
	}

	res, err := exportpkg.Liked(s.Store, exportpkg.Options{
		Destination: req.Destination,
		BatchSize:   req.BatchSize,
		MaxBytes:    req.MaxBytes,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "EXPORT_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, models.ExportLikedResponse{
		Success:     res.Failed == 0,
		Destination: res.Destination,
		BatchSize:   res.BatchSize,
		MaxBytes:    res.MaxBytes,
		Exported:    res.Exported,
		Failed:      res.Failed,
		Batches:     res.Batches,
		BatchDirs:   res.BatchDirs,
		Errors:      res.Errors,
	})
}
