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
	size    preview.Size
	total   int
	ready   int
	pending int
	failed  int
	running bool
}

// warmDoneEntry remembers a finished warmup fingerprint per bucket+size so
// repeat opens don't re-stat every cached JPEG.
type warmDoneEntry struct {
	fp    string
	total int
}

func warmDoneKey(bucket models.Bucket, size preview.Size) string {
	return string(bucket) + "|" + string(size)
}

func parseWarmupSize(v string) preview.Size {
	if strings.EqualFold(v, string(preview.SizeView)) {
		return preview.SizeView
	}
	return preview.SizeThumb
}

func (s *Server) previewStatusSnapshot() models.PreviewWarmupStatus {
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	return models.PreviewWarmupStatus{
		Bucket:  s.warm.bucket,
		Size:    string(s.warm.size),
		Total:   s.warm.total,
		Ready:   s.warm.ready,
		Pending: s.warm.pending,
		Failed:  s.warm.failed,
		Running: s.warm.running,
	}
}

func (s *Server) handlePreviewStatus(w http.ResponseWriter, r *http.Request) {
	bucket := models.Bucket(r.URL.Query().Get("bucket"))
	size := parseWarmupSize(r.URL.Query().Get("size"))
	if bucket == "" {
		writeJSON(w, http.StatusOK, s.previewStatusSnapshot())
		return
	}
	if !bucket.LibraryBucket() {
		writeError(w, http.StatusBadRequest, "INVALID_BUCKET", "Invalid library bucket")
		return
	}

	cur := s.previewStatusSnapshot()
	if cur.Running && cur.Bucket == bucket && cur.Size == string(size) {
		writeJSON(w, http.StatusOK, cur)
		return
	}

	if status, ok := s.cachedWarmStatus(bucket, size); ok {
		writeJSON(w, http.StatusOK, status)
		return
	}

	status, err := s.inspectWarmup(bucket, size)
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
	size := parseWarmupSize(r.URL.Query().Get("size"))
	status, err := s.startWarmup(bucket, size)
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

func (s *Server) cachedWarmStatus(bucket models.Bucket, size preview.Size) (models.PreviewWarmupStatus, bool) {
	names, err := s.listBucketImageNames(bucket)
	if err != nil {
		return models.PreviewWarmupStatus{}, false
	}
	fp := bucketImageFingerprint(names)
	key := warmDoneKey(bucket, size)
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	done, ok := s.warmDone[key]
	if !ok || done.fp != fp {
		return models.PreviewWarmupStatus{}, false
	}
	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Size:    string(size),
		Total:   done.total,
		Ready:   done.total,
		Pending: 0,
		Failed:  0,
		Running: false,
	}, true
}

func (s *Server) markWarmDone(bucket models.Bucket, size preview.Size, names []string) {
	fp := bucketImageFingerprint(names)
	s.warmMu.Lock()
	s.warmDone[warmDoneKey(bucket, size)] = warmDoneEntry{fp: fp, total: len(names)}
	s.warmMu.Unlock()
}

func (s *Server) invalidateWarmDone(buckets ...models.Bucket) {
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	if len(buckets) == 0 {
		s.warmDone = make(map[string]warmDoneEntry)
		return
	}
	for _, b := range buckets {
		delete(s.warmDone, warmDoneKey(b, preview.SizeThumb))
		delete(s.warmDone, warmDoneKey(b, preview.SizeView))
	}
}

func (s *Server) inspectWarmup(bucket models.Bucket, size preview.Size) (models.PreviewWarmupStatus, error) {
	paths, err := s.Store.ListBucketImagePaths(bucket)
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}
	ready := 0
	for _, p := range paths {
		if preview.HasCachedJPEG(s.Store.Root, p, size) {
			ready++
		}
	}
	total := len(paths)
	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Size:    string(size),
		Total:   total,
		Ready:   ready,
		Pending: total - ready,
		Failed:  0,
		Running: false,
	}, nil
}

func (s *Server) startWarmup(bucket models.Bucket, size preview.Size) (models.PreviewWarmupStatus, error) {
	if status, ok := s.cachedWarmStatus(bucket, size); ok {
		s.warmMu.Lock()
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			bucket:  bucket,
			size:    size,
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
	if s.warm.running && s.warm.bucket == bucket && s.warm.size == size {
		out := models.PreviewWarmupStatus{
			Bucket:  s.warm.bucket,
			Size:    string(s.warm.size),
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

	status, err := s.inspectWarmup(bucket, size)
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}

	s.warmMu.Lock()
	if status.Pending == 0 {
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			bucket:  bucket,
			size:    size,
			total:   status.Total,
			ready:   status.Ready,
			pending: 0,
			failed:  0,
			running: false,
		}
		s.warmMu.Unlock()
		names, _ := s.listBucketImageNames(bucket)
		s.markWarmDone(bucket, size, names)
		return models.PreviewWarmupStatus{
			Bucket:  bucket,
			Size:    string(size),
			Total:   status.Total,
			Ready:   status.Ready,
			Pending: 0,
			Failed:  0,
			Running: false,
		}, nil
	}

	if s.warm.running && s.warm.bucket == bucket && s.warm.size == size {
		out := models.PreviewWarmupStatus{
			Bucket:  s.warm.bucket,
			Size:    string(s.warm.size),
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
	s.warm.size = size
	s.warm.total = status.Total
	s.warm.ready = status.Ready
	s.warm.pending = status.Pending
	s.warm.failed = 0
	s.warm.running = true
	s.warmMu.Unlock()

	go s.runWarmup(gen, bucket, size)

	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Size:    string(size),
		Total:   status.Total,
		Ready:   status.Ready,
		Pending: status.Pending,
		Failed:  0,
		Running: true,
	}, nil
}

func (s *Server) runWarmup(gen uint64, bucket models.Bucket, size preview.Size) {
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
		if preview.HasCachedJPEG(s.Store.Root, p, size) {
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
		s.markWarmDone(bucket, size, names)
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
				_, err := preview.DisplayJPEG(s.Store.Root, path, size)
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
		s.markWarmDone(bucket, size, names)
	}
}
