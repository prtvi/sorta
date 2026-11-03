package server

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/prtvi/sorta/internal/meta"
	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/preview"
	"github.com/prtvi/sorta/internal/scanner"
)

const (
	warmScopeDirectory = "directory"
	warmSizeBoth       = "both"
	warmDoneDirectory  = "__directory__|both"
)

type thumbWarmState struct {
	gen     uint64
	scope   string // "directory" or a single bucket name
	bucket  models.Bucket
	size    preview.Size
	total   int
	ready   int
	pending int
	failed  int
	running bool
}

func warmDoneKey(bucket models.Bucket, size preview.Size) string {
	return string(bucket) + "|" + string(size)
}

func parseWarmupSize(v string) preview.Size {
	if strings.EqualFold(v, string(preview.SizeView)) {
		return preview.SizeView
	}
	if strings.EqualFold(v, warmSizeBoth) {
		return preview.Size(warmSizeBoth)
	}
	return preview.SizeThumb
}

func (s *Server) previewStatusSnapshot() models.PreviewWarmupStatus {
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	size := string(s.warm.size)
	if s.warm.scope == warmScopeDirectory {
		size = warmSizeBoth
	}
	return models.PreviewWarmupStatus{
		Bucket:  s.warm.bucket,
		Scope:   s.warm.scope,
		Size:    size,
		Total:   s.warm.total,
		Ready:   s.warm.ready,
		Pending: s.warm.pending,
		Failed:  s.warm.failed,
		Running: s.warm.running,
	}
}

func (s *Server) handlePreviewStatus(w http.ResponseWriter, r *http.Request) {
	scope := strings.ToLower(r.URL.Query().Get("scope"))
	if scope == warmScopeDirectory {
		cur := s.previewStatusSnapshot()
		if cur.Running && cur.Scope == warmScopeDirectory {
			writeJSON(w, http.StatusOK, cur)
			return
		}
		if status, ok := s.cachedDirectoryWarmStatus(); ok {
			writeJSON(w, http.StatusOK, status)
			return
		}
		status, err := s.inspectDirectoryWarmup()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not scan photos for previews")
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

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
	if cur.Running && cur.Scope == warmScopeDirectory {
		// Directory warmup covers every bucket — surface its progress.
		writeJSON(w, http.StatusOK, cur)
		return
	}
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
	if strings.EqualFold(r.URL.Query().Get("cancel"), "1") ||
		strings.EqualFold(r.URL.Query().Get("cancel"), "true") {
		s.cancelWarmup()
		writeJSON(w, http.StatusOK, s.previewStatusSnapshot())
		return
	}

	scope := strings.ToLower(r.URL.Query().Get("scope"))
	if scope == warmScopeDirectory || r.URL.Query().Get("size") == warmSizeBoth {
		status, err := s.startDirectoryWarmup()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SCAN_FAILED", "Could not start preview warmup")
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}

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

// warmupBuckets are scanned for directory-wide thumb+view generation.
// Posted is Finder-managed archive — warmed on demand when browsing that tab.
func warmupBuckets() []models.Bucket {
	return []models.Bucket{
		models.BucketRoot,
		models.BucketLiked,
		models.BucketShortlisted,
		models.BucketPosting,
		models.BucketSharing,
		models.BucketDisliked,
	}
}

func (s *Server) listWarmupImagePaths() ([]string, map[models.Bucket][]string, error) {
	buckets := warmupBuckets()
	all := make([]string, 0, 256)
	byBucket := make(map[models.Bucket][]string, len(buckets))
	for _, b := range buckets {
		paths, err := s.Store.ListBucketImagePaths(b)
		if err != nil {
			return nil, nil, err
		}
		names := make([]string, len(paths))
		for i, p := range paths {
			names[i] = filepath.Base(p)
		}
		byBucket[b] = names
		all = append(all, paths...)
	}
	return all, byBucket, nil
}

func directoryImageFingerprint(byBucket map[models.Bucket][]string) string {
	lines := make([]string, 0, 256)
	for _, b := range warmupBuckets() {
		names := append([]string(nil), byBucket[b]...)
		sort.Strings(names)
		for _, n := range names {
			lines = append(lines, string(b)+"/"+n)
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Server) loadWarmDoneFromDisk() {
	if s.Store == nil {
		return
	}
	f, err := meta.Read(s.Store.Root)
	s.warmMu.Lock()
	if err != nil || f.WarmDone == nil {
		s.warmDone = make(map[string]meta.WarmDoneEntry)
	} else {
		s.warmDone = f.WarmDone
	}
	s.warmMu.Unlock()
}

// persistWarmDone writes the in-memory warmDone map into .sorta/meta.json.
// Must not be called while holding warmMu (takes warmMu itself after persistMu).
func (s *Server) persistWarmDone() {
	if s.Store == nil {
		return
	}
	s.warmPersistMu.Lock()
	defer s.warmPersistMu.Unlock()

	s.warmMu.Lock()
	cp := make(map[string]meta.WarmDoneEntry, len(s.warmDone))
	for k, v := range s.warmDone {
		cp[k] = v
	}
	root := s.Store.Root
	s.warmMu.Unlock()

	_ = meta.Update(root, func(f *meta.File) error {
		f.WarmDone = cp
		return nil
	})
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
	if !ok || done.FP != fp {
		return models.PreviewWarmupStatus{}, false
	}
	return models.PreviewWarmupStatus{
		Bucket:  bucket,
		Size:    string(size),
		Total:   done.Total,
		Ready:   done.Total,
		Pending: 0,
		Failed:  0,
		Running: false,
	}, true
}

func (s *Server) cachedDirectoryWarmStatus() (models.PreviewWarmupStatus, bool) {
	_, byBucket, err := s.listWarmupImagePaths()
	if err != nil {
		return models.PreviewWarmupStatus{}, false
	}
	fp := directoryImageFingerprint(byBucket)
	total := 0
	for _, names := range byBucket {
		total += len(names)
	}
	s.warmMu.Lock()
	defer s.warmMu.Unlock()
	done, ok := s.warmDone[warmDoneDirectory]
	if !ok || done.FP != fp {
		return models.PreviewWarmupStatus{}, false
	}
	return models.PreviewWarmupStatus{
		Bucket:  models.BucketRoot,
		Scope:   warmScopeDirectory,
		Size:    warmSizeBoth,
		Total:   total,
		Ready:   total,
		Pending: 0,
		Failed:  0,
		Running: false,
	}, true
}

func (s *Server) markWarmDone(bucket models.Bucket, size preview.Size, names []string) {
	fp := bucketImageFingerprint(names)
	s.warmMu.Lock()
	s.warmDone[warmDoneKey(bucket, size)] = meta.WarmDoneEntry{FP: fp, Total: len(names)}
	s.warmMu.Unlock()
	s.persistWarmDone()
}

func (s *Server) markDirectoryWarmDone(byBucket map[models.Bucket][]string) {
	fp := directoryImageFingerprint(byBucket)
	total := 0
	s.warmMu.Lock()
	for _, b := range warmupBuckets() {
		names := byBucket[b]
		if names == nil {
			names = []string{}
		}
		total += len(names)
		bfp := bucketImageFingerprint(names)
		s.warmDone[warmDoneKey(b, preview.SizeThumb)] = meta.WarmDoneEntry{FP: bfp, Total: len(names)}
		s.warmDone[warmDoneKey(b, preview.SizeView)] = meta.WarmDoneEntry{FP: bfp, Total: len(names)}
	}
	s.warmDone[warmDoneDirectory] = meta.WarmDoneEntry{FP: fp, Total: total}
	s.warmMu.Unlock()
	s.persistWarmDone()
}

// cancelWarmup stops any in-flight warmup without writing a durable warm-done mark.
func (s *Server) cancelWarmup() {
	s.warmMu.Lock()
	s.warm.gen++
	s.warm.running = false
	s.warmMu.Unlock()
}

func (s *Server) invalidateDirectoryWarmMark() {
	s.warmMu.Lock()
	delete(s.warmDone, warmDoneDirectory)
	s.warmMu.Unlock()
	s.persistWarmDone()
}

func (s *Server) invalidateWarmDone(buckets ...models.Bucket) {
	s.warmMu.Lock()
	if len(buckets) == 0 {
		s.warmDone = make(map[string]meta.WarmDoneEntry)
	} else {
		delete(s.warmDone, warmDoneDirectory)
		for _, b := range buckets {
			delete(s.warmDone, warmDoneKey(b, preview.SizeThumb))
			delete(s.warmDone, warmDoneKey(b, preview.SizeView))
		}
	}
	s.warmMu.Unlock()
	s.persistWarmDone()
}

func (s *Server) inspectWarmup(bucket models.Bucket, size preview.Size) (models.PreviewWarmupStatus, error) {
	paths, err := s.Store.ListBucketImagePaths(bucket)
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}
	ready := 0
	for _, p := range paths {
		if size == preview.Size(warmSizeBoth) {
			if !preview.NeedsBoth(s.Store.Root, p) {
				ready++
			}
			continue
		}
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

func (s *Server) inspectDirectoryWarmup() (models.PreviewWarmupStatus, error) {
	paths, _, err := s.listWarmupImagePaths()
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}
	ready := 0
	for _, p := range paths {
		if !preview.NeedsBoth(s.Store.Root, p) {
			ready++
		}
	}
	total := len(paths)
	return models.PreviewWarmupStatus{
		Bucket:  models.BucketRoot,
		Scope:   warmScopeDirectory,
		Size:    warmSizeBoth,
		Total:   total,
		Ready:   ready,
		Pending: total - ready,
		Failed:  0,
		Running: false,
	}, nil
}

func (s *Server) startDirectoryWarmup() (models.PreviewWarmupStatus, error) {
	if status, ok := s.cachedDirectoryWarmStatus(); ok {
		s.warmMu.Lock()
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			scope:   warmScopeDirectory,
			bucket:  models.BucketRoot,
			size:    preview.Size(warmSizeBoth),
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
	if s.warm.running && s.warm.scope == warmScopeDirectory {
		out := models.PreviewWarmupStatus{
			Bucket:  s.warm.bucket,
			Scope:   warmScopeDirectory,
			Size:    warmSizeBoth,
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

	status, err := s.inspectDirectoryWarmup()
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}

	s.warmMu.Lock()
	if status.Pending == 0 {
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			scope:   warmScopeDirectory,
			bucket:  models.BucketRoot,
			size:    preview.Size(warmSizeBoth),
			total:   status.Total,
			ready:   status.Ready,
			pending: 0,
			failed:  0,
			running: false,
		}
		s.warmMu.Unlock()
		_, byBucket, _ := s.listWarmupImagePaths()
		s.markDirectoryWarmDone(byBucket)
		return status, nil
	}

	if s.warm.running && s.warm.scope == warmScopeDirectory {
		out := models.PreviewWarmupStatus{
			Bucket:  s.warm.bucket,
			Scope:   warmScopeDirectory,
			Size:    warmSizeBoth,
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
	s.warm.scope = warmScopeDirectory
	s.warm.bucket = models.BucketRoot
	s.warm.size = preview.Size(warmSizeBoth)
	s.warm.total = status.Total
	s.warm.ready = status.Ready
	s.warm.pending = status.Pending
	s.warm.failed = 0
	s.warm.running = true
	s.warmMu.Unlock()

	go s.runDirectoryWarmup(gen)

	return models.PreviewWarmupStatus{
		Bucket:  models.BucketRoot,
		Scope:   warmScopeDirectory,
		Size:    warmSizeBoth,
		Total:   status.Total,
		Ready:   status.Ready,
		Pending: status.Pending,
		Failed:  0,
		Running: true,
	}, nil
}

func (s *Server) runDirectoryWarmup(gen uint64) {
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

	paths, byBucket, err := s.listWarmupImagePaths()
	if err != nil {
		return
	}

	pending := make([]string, 0, len(paths))
	ready := 0
	for _, p := range paths {
		if preview.NeedsBoth(s.Store.Root, p) {
			pending = append(pending, p)
			continue
		}
		ready++
	}

	s.warmMu.Lock()
	if s.warm.gen == gen {
		s.warm.total = len(paths)
		s.warm.ready = ready
		s.warm.pending = len(pending)
		s.warm.failed = 0
	}
	alive := s.warm.gen == gen
	s.warmMu.Unlock()
	if !alive {
		return
	}

	if len(pending) == 0 {
		s.markDirectoryWarmDone(byBucket)
		return
	}

	s.runWarmPool(gen, pending)

	s.warmMu.Lock()
	finishedClean := s.warm.gen == gen && s.warm.pending == 0 && s.warm.failed == 0
	s.warmMu.Unlock()
	// Only persist warm-done when every pending convert succeeded — failed
	// files must be retried on the next boot.
	if finishedClean {
		s.markDirectoryWarmDone(byBucket)
	}
}

func (s *Server) startWarmup(bucket models.Bucket, size preview.Size) (models.PreviewWarmupStatus, error) {
	// Prefer directory-wide completion — avoids redoing work per tab.
	if status, ok := s.cachedDirectoryWarmStatus(); ok {
		names, _ := s.listBucketImageNames(bucket)
		n := len(names)
		out := models.PreviewWarmupStatus{
			Bucket:  bucket,
			Scope:   warmScopeDirectory,
			Size:    string(size),
			Total:   n,
			Ready:   n,
			Pending: 0,
			Failed:  0,
			Running: false,
		}
		_ = status
		return out, nil
	}

	if status, ok := s.cachedWarmStatus(bucket, size); ok {
		s.warmMu.Lock()
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			scope:   string(bucket),
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
	if s.warm.running && s.warm.scope == warmScopeDirectory {
		out := models.PreviewWarmupStatus{
			Bucket:  bucket,
			Scope:   warmScopeDirectory,
			Size:    warmSizeBoth,
			Total:   s.warm.total,
			Ready:   s.warm.ready,
			Pending: s.warm.pending,
			Failed:  s.warm.failed,
			Running: true,
		}
		s.warmMu.Unlock()
		return out, nil
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
	s.warmMu.Unlock()

	status, err := s.inspectWarmup(bucket, size)
	if err != nil {
		return models.PreviewWarmupStatus{}, err
	}

	s.warmMu.Lock()
	if status.Pending == 0 {
		s.warm = thumbWarmState{
			gen:     s.warm.gen + 1,
			scope:   string(bucket),
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
	s.warm.scope = string(bucket)
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
		need := false
		if size == preview.Size(warmSizeBoth) {
			need = preview.NeedsBoth(s.Store.Root, p)
		} else if !preview.HasCachedJPEG(s.Store.Root, p, size) {
			need = true
		}
		if need {
			pending = append(pending, p)
			continue
		}
		ready++
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

	s.runWarmPoolSized(gen, pending, size)

	s.warmMu.Lock()
	finishedClean := s.warm.gen == gen && s.warm.pending == 0 && s.warm.failed == 0
	s.warmMu.Unlock()
	if finishedClean {
		names := make([]string, len(paths))
		for i, p := range paths {
			names[i] = filepath.Base(p)
		}
		s.markWarmDone(bucket, size, names)
	}
}

// runWarmPool generates thumb+view for each pending path (directory warmup).
func (s *Server) runWarmPool(gen uint64, pending []string) {
	workers := preview.WarmWorkers()
	jobs := make(chan string, workers*2)
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
				_, _, err := preview.EnsureBoth(s.Store.Root, path)
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
}

// runWarmPoolSized generates a single preview size (legacy per-bucket warmup).
func (s *Server) runWarmPoolSized(gen uint64, pending []string, size preview.Size) {
	workers := preview.WarmWorkers()
	jobs := make(chan string, workers*2)
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
				var err error
				if size == preview.Size(warmSizeBoth) {
					_, _, err = preview.EnsureBoth(s.Store.Root, path)
				} else {
					_, err = preview.DisplayJPEG(s.Store.Root, path, size)
				}
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
}
