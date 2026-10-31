package server

import (
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/preview"
	"github.com/prtvi/sorta/internal/scanner"
)

type thumbWarmState struct {
	gen     uint64
	bucket  models.Bucket
	total   int
	ready   int
	pending int
	failed  int
	running bool
}

// warmDoneEntry remembers a finished warmup fingerprint per bucket so repeat
// Library opens don't re-stat every cached JPEG.
type warmDoneEntry struct {
	fp    string
	total int
}

func (s *Server) previewStatusSnapshot() models.PreviewWarmupStatus {
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	return models.PreviewWarmupStatus{
		Bucket:  s.warm.bucket,
		Total:   s.warm.total,
		Ready:   s.warm.ready,
		Pending: s.warm.pending,
		Failed:  s.warm.failed,
		Running: s.warm.running,
	}
}

func (s *Server) handlePreviewStatus(w http.ResponseWriter, r *http.Request) {
	bucket := models.Bucket(r.URL.Query().Get("bucket"))
	if bucket == "" {
		writeJSON(w, http.StatusOK, s.previewStatusSnapshot())
		return
	}
	if !bucket.LibraryBucket() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid library bucket")
		return
	}

	cur := s.previewStatusSnapshot()
	if cur.Running && cur.Bucket == bucket {
		writeJSON(w, http.StatusOK, cur)
		return
	}

	// Live counters without a bucket filter, or idle — prefer done-cache / snapshot.
	if status, ok := s.cachedWarmStatus(bucket); ok {
		writeJSON(w, http.StatusOK, status)
		return
	}

	status, err := s.inspectThumbWarmup(bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan photos for previews")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handlePreviewWarmup(w http.ResponseWriter, r *http.Request) {
	bucket := models.Bucket(r.URL.Query().Get("bucket"))
	if bucket == "" {
		bucket = models.BucketRoot
	}
	if !bucket.LibraryBucket() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid library bucket")
		return
	}
	status, err := s.startThumbWarmup(bucket)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not start preview warmup")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func bucketImageFingerprint(names []string) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return strings.Join(sorted, "\n")
}

func (s *Server) listBucketImageNames(bucket models.Bucket) ([]string, error) {
	dir, err := s.Store.BucketDir(bucket)
	if err != nil {
		return nil, err
	}
	return scanner.ListImageNames(dir)
}

func (s *Server) cachedWarmStatus(bucket models.Bucket) (models.PreviewWarmupStatus, bool) {
	names, err := s.listBucketImageNames(bucket)
	if err != nil {
		return models.PreviewWarmupStatus{}, false
	}
	fp := bucketImageFingerprint(names)
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	done, ok := s.warmDone[bucket]
	if !ok || done.fp != fp {
		return models.PreviewWarmupStatus{}, false
	}
	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Total:   done.total,
		Ready:   done.total,
		Pending: 0,
		Failed:  0,
		Running: false,
	}, true
}

func (s *Server) markWarmDone(bucket models.Bucket, names []string) {
	fp := bucketImageFingerprint(names)
	s.warmMu.Lock()
	s.warmDone[bucket] = warmDoneEntry{fp: fp, total: len(names)}
	s.warmMu.Unlock()
}

func (s *Server) invalidateWarmDone(buckets ...models.Bucket) {
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	if len(buckets) == 0 {
		s.warmDone = make(map[models.Bucket]warmDoneEntry)
		return
	}
	for _, b := range buckets {
		delete(s.warmDone, b)
	}
}

func (s *Server) inspectThumbWarmup(bucket models.Bucket) (models.PreviewWarmupStatus, error) {
	paths, err := s.Store.ListBucketImagePaths(bucket)
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}
	ready := 0
	for _, p := range paths {
		if preview.HasCachedJPEG(s.Store.Root, p, preview.SizeThumb) {
			ready++
		}
	}
	total := len(paths)
	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Total:   total,
		Ready:   ready,
		Pending: total - ready,
		Failed:  0,
		Running: false,
	}, nil
}

func (s *Server) startThumbWarmup(bucket models.Bucket) (models.PreviewWarmupStatus, error) {
	// Fast path: names-only fingerprint — skip per-file cache stats when unchanged.
	if status, ok := s.cachedWarmStatus(bucket); ok {
		s.warmMu.Lock()
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			bucket:  bucket,
			total:   status.Total,
			ready:   status.Ready,
			pending: 0,
			failed:  0,
			running: false,
		}
		s.warmMu.Unlock()
		return status, nil
	}

	s.warmMu.Lock()
	// Same bucket already warming — return live counters.
	if s.warm.running && s.warm.bucket == bucket {
		out := models.PreviewWarmupStatus{
			Bucket:  s.warm.bucket,
			Total:   s.warm.total,
			Ready:   s.warm.ready,
			Pending: s.warm.pending,
			Failed:  s.warm.failed,
			Running: true,
		}
		s.warmMu.Unlock()
		return out, nil
	}
	s.warmMu.Unlock()

	status, err := s.inspectThumbWarmup(bucket)
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}

	s.warmMu.Lock()
	if status.Pending == 0 {
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			bucket:  bucket,
			total:   status.Total,
			ready:   status.Ready,
			pending: 0,
			failed:  0,
			running: false,
		}
		s.warmMu.Unlock()
		names, _ := s.listBucketImageNames(bucket)
		s.markWarmDone(bucket, names)
		return models.PreviewWarmupStatus{
			Bucket:  bucket,
			Total:   status.Total,
			Ready:   status.Ready,
			Pending: 0,
			Failed:  0,
			Running: false,
		}, nil
	}

	// Re-check running under lock after inspect (another request may have started).
	if s.warm.running && s.warm.bucket == bucket {
		out := models.PreviewWarmupStatus{
			Bucket:  s.warm.bucket,
			Total:   s.warm.total,
			Ready:   s.warm.ready,
			Pending: s.warm.pending,
			Failed:  s.warm.failed,
			Running: true,
		}
		s.warmMu.Unlock()
		return out, nil
	}

	s.warm.gen++
	gen := s.warm.gen
	s.warm.bucket = bucket
	s.warm.total = status.Total
	s.warm.ready = status.Ready
	s.warm.pending = status.Pending
	s.warm.failed = 0
	s.warm.running = true
	s.warmMu.Unlock()

	go s.runThumbWarmup(gen, bucket)

	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Total:   status.Total,
		Ready:   status.Ready,
		Pending: status.Pending,
		Failed:  0,
		Running: true,
	}, nil
}

func (s *Server) runThumbWarmup(gen uint64, bucket models.Bucket) {
	defer func() {
		s.warmMu.Lock()
		if s.warm.gen == gen {
			s.warm.running = false
			if s.warm.pending < 0 {
				s.warm.pending = 0
			}
		}
		s.warmMu.Unlock()
	}()

	paths, err := s.Store.ListBucketImagePaths(bucket)
	if err != nil {
		return
	}

	pending := make([]string, 0, len(paths))
	ready := 0
	for _, p := range paths {
		if preview.HasCachedJPEG(s.Store.Root, p, preview.SizeThumb) {
			ready++
			continue
		}
		pending = append(pending, p)
	}

	s.warmMu.Lock()
	if s.warm.gen != gen {
		s.warmMu.Unlock()
		return
	}
	s.warm.total = len(paths)
	s.warm.ready = ready
	s.warm.pending = len(pending)
	s.warm.failed = 0
	s.warmMu.Unlock()

	if len(pending) == 0 {
		names := make([]string, len(paths))
		for i, p := range paths {
			names[i] = filepath.Base(p)
		}
		s.markWarmDone(bucket, names)
		return
	}

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 4 {
		workers = 4
	}

	jobs := make(chan string, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				s.warmMu.Lock()
				alive := s.warm.gen == gen
				s.warmMu.Unlock()
				if !alive {
					return
				}
				_, err := preview.DisplayJPEG(s.Store.Root, path, preview.SizeThumb)
				s.warmMu.Lock()
				if s.warm.gen != gen {
					s.warmMu.Unlock()
					return
				}
				if err != nil {
					s.warm.failed++
				} else {
					s.warm.ready++
				}
				s.warm.pending--
				if s.warm.pending < 0 {
					s.warm.pending = 0
				}
				s.warmMu.Unlock()
			}
		}()
	}

	for _, path := range pending {
		s.warmMu.Lock()
		alive := s.warm.gen == gen
		s.warmMu.Unlock()
		if !alive {
			break
		}
		jobs <- path
	}
	close(jobs)
	wg.Wait()

	s.warmMu.Lock()
	finished := s.warm.gen == gen && s.warm.pending == 0
	s.warmMu.Unlock()
	if finished {
		names := make([]string, len(paths))
		for i, p := range paths {
			names[i] = filepath.Base(p)
		}
		s.markWarmDone(bucket, names)
	}
}
