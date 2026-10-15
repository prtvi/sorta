package export

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/prtvi/sorta/internal/filesystem"
	"github.com/prtvi/sorta/internal/models"
	"github.com/prtvi/sorta/internal/preview"
)

const (
	DefaultBatchSize = 30
	DefaultMaxBytes  = 15 * 1024 * 1024 // 15MB

	// ModeCompressed re-encodes each photo as a JPEG ≤ MaxBytes.
	ModeCompressed = "compressed"
	// ModeOriginal copies each photo byte-for-byte with its original name and format.
	ModeOriginal = "original"
)

// Options controls a bucket photo export.
type Options struct {
	Bucket      models.Bucket
	Destination string
	BatchSize   int
	MaxBytes    int64
	// Mode is ModeCompressed (default) or ModeOriginal.
	Mode string
	// Names, when non-empty, limits the export to those filenames in the bucket.
	Names []string
}

// Result summarizes an export run.
type Result struct {
	Destination string   `json:"destination"`
	BatchSize   int      `json:"batch_size"`
	MaxBytes    int64    `json:"max_bytes"`
	Mode        string   `json:"mode"`
	Exported    int      `json:"exported"`
	Failed      int      `json:"failed"`
	Batches     int      `json:"batches"`
	BatchDirs   []string `json:"batch_dirs"`
	Errors      []string `json:"errors,omitempty"`
}

// NormalizeMode returns a valid export mode, defaulting empty to compressed.
func NormalizeMode(mode string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", ModeCompressed:
		return ModeCompressed, nil
	case ModeOriginal:
		return ModeOriginal, nil
	default:
		return "", fmt.Errorf("invalid export mode %q (use %q or %q)", mode, ModeCompressed, ModeOriginal)
	}
}

// Exportable reports whether photos from b may be batch-exported.
func Exportable(b models.Bucket) bool {
	switch b {
	case models.BucketLiked, models.BucketShortlisted, models.BucketPosting, models.BucketSharing:
		return true
	default:
		return false
	}
}

// Bucket exports photos from a library bucket into destination/batch-NNN/ folders.
// ModeCompressed writes JPEGs ≤ maxBytes; ModeOriginal copies files unchanged.
// Originals in the working tree are never modified.
func Bucket(store *filesystem.Store, opts Options) (*Result, error) {
	if !Exportable(opts.Bucket) {
		return nil, fmt.Errorf("export not allowed for bucket %q", opts.Bucket)
	}
	mode, err := NormalizeMode(opts.Mode)
	if err != nil {
		return nil, err
	}
	opts.Mode = mode
	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}
	if opts.Mode == ModeCompressed && opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	dest, err := sanitizeDestination(opts.Destination)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("create destination: %w", err)
	}

	photos, err := store.ListLibraryBucket(opts.Bucket)
	if err != nil {
		return nil, err
	}
	if len(opts.Names) > 0 {
		want := make(map[string]struct{}, len(opts.Names))
		for _, name := range opts.Names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			want[name] = struct{}{}
		}
		filtered := make([]models.Photo, 0, len(want))
		for _, p := range photos {
			if _, ok := want[p.Name]; ok {
				filtered = append(filtered, p)
				delete(want, p.Name)
			}
		}
		if len(want) > 0 {
			missing := make([]string, 0, len(want))
			for name := range want {
				missing = append(missing, name)
			}
			return nil, fmt.Errorf("photos not in %s: %s", opts.Bucket, strings.Join(missing, ", "))
		}
		photos = filtered
	}
	srcDir, err := store.BucketDir(opts.Bucket)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Destination: dest,
		BatchSize:   opts.BatchSize,
		MaxBytes:    opts.MaxBytes,
		Mode:        opts.Mode,
		BatchDirs:   []string{},
	}
	if len(photos) == 0 {
		return res, nil
	}

	batchCount := (len(photos) + opts.BatchSize - 1) / opts.BatchSize
	for b := 0; b < batchCount; b++ {
		batchName := fmt.Sprintf("batch-%03d", b+1)
		batchDir := filepath.Join(dest, batchName)
		if err := os.MkdirAll(batchDir, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", batchName, err)
		}
		res.BatchDirs = append(res.BatchDirs, batchName)
		res.Batches++

		start := b * opts.BatchSize
		end := start + opts.BatchSize
		if end > len(photos) {
			end = len(photos)
		}
		for _, p := range photos[start:end] {
			src := filepath.Join(srcDir, p.Name)
			var outName string
			var writeErr error
			if opts.Mode == ModeOriginal {
				outName = p.Name
				outPath := filepath.Join(batchDir, outName)
				writeErr = copyFile(src, outPath)
			} else {
				outName = exportJPEGName(p.Name)
				outPath := filepath.Join(batchDir, outName)
				writeErr = CompressToMaxBytes(src, outPath, opts.MaxBytes)
			}
			if writeErr != nil {
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p.Name, writeErr))
				continue
			}
			res.Exported++
		}
	}
	return res, nil
}

func sanitizeDestination(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("destination path is required")
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		if path == "~" {
			path = home
		} else if strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid destination: %w", err)
	}
	return abs, nil
}

func exportJPEGName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return stem + ".jpg"
	default:
		return stem + ".jpg"
	}
}

// CompressToMaxBytes writes a JPEG copy of src to dest with size ≤ maxBytes when possible.
func CompressToMaxBytes(src, dest string, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Fast path: already a small-enough JPEG — copy as-is.
	ext := strings.ToLower(filepath.Ext(src))
	if (ext == ".jpg" || ext == ".jpeg") && info.Size() <= maxBytes {
		return copyFile(src, dest)
	}

	qualities := []int{90, 85, 80, 75, 70, 65, 60, 55, 50, 45, 40}
	edges := []int{6000, 5000, 4000, 3200, 2560, 2048, 1600, 1280}

	tmpDir := filepath.Dir(dest)
	tmp := filepath.Join(tmpDir, "."+filepath.Base(dest)+".tmp")
	defer os.Remove(tmp)

	var lastErr error
	for _, edge := range edges {
		for _, q := range qualities {
			if err := encodeJPEG(src, tmp, edge, q); err != nil {
				lastErr = err
				continue
			}
			st, err := os.Stat(tmp)
			if err != nil {
				lastErr = err
				continue
			}
			if st.Size() <= maxBytes {
				_ = os.Remove(dest)
				if err := os.Rename(tmp, dest); err != nil {
					return copyFile(tmp, dest)
				}
				return nil
			}
			lastErr = fmt.Errorf("still %d bytes after q=%d edge=%d", st.Size(), q, edge)
		}
	}

	// Best effort: keep the last encoded file even if slightly over (should be rare).
	if _, err := os.Stat(tmp); err == nil {
		_ = os.Remove(dest)
		if err := os.Rename(tmp, dest); err != nil {
			_ = copyFile(tmp, dest)
		}
		if st, err := os.Stat(dest); err == nil && st.Size() <= maxBytes {
			return nil
		}
		return fmt.Errorf("could not get under %d bytes: %v", maxBytes, lastErr)
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("compression failed")
}

func encodeJPEG(src, dest string, maxEdge, quality int) error {
	switch runtime.GOOS {
	case "darwin":
		return encodeSips(src, dest, maxEdge, quality)
	default:
		err := encodeMagick(src, dest, maxEdge, quality)
		if err != nil && preview.NeedsTranscode(src) {
			mid := dest + ".heicconv.jpg"
			defer os.Remove(mid)
			if err2 := heifConvert(src, mid); err2 != nil {
				return err
			}
			return encodeMagick(mid, dest, maxEdge, quality)
		}
		return err
	}
}

func encodeSips(src, dest string, maxEdge, quality int) error {
	cmd := exec.Command(
		"sips",
		"-Z", fmt.Sprintf("%d", maxEdge),
		"-s", "format", "jpeg",
		"-s", "formatOptions", fmt.Sprintf("%d", quality),
		src,
		"--out", dest,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sips: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func encodeMagick(src, dest string, maxEdge, quality int) error {
	bin, err := exec.LookPath("magick")
	if err != nil {
		bin, err = exec.LookPath("convert")
		if err != nil {
			return fmt.Errorf("no image converter found")
		}
	}
	geom := fmt.Sprintf("%dx%d>", maxEdge, maxEdge)
	cmd := exec.Command(bin, src, "-auto-orient", "-resize", geom, "-quality", fmt.Sprintf("%d", quality), dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func heifConvert(src, dest string) error {
	path, err := exec.LookPath("heif-convert")
	if err != nil {
		return err
	}
	cmd := exec.Command(path, src, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("heif-convert: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
