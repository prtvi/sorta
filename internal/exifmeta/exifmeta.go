package exifmeta

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
)

// Metadata is display-oriented EXIF (no GPS coordinates by default).
type Metadata struct {
	Camera      string `json:"camera,omitempty"`
	Lens        string `json:"lens,omitempty"`
	FocalLength string `json:"focal_length,omitempty"`
	Aperture    string `json:"aperture,omitempty"`
	Shutter     string `json:"shutter,omitempty"`
	ISO         string `json:"iso,omitempty"`
	TakenAt     string `json:"taken_at,omitempty"`
	HasGPS      bool   `json:"has_gps"`
}

// Empty reports whether there is nothing useful to show.
func (m Metadata) Empty() bool {
	return m.Camera == "" && m.Lens == "" && m.FocalLength == "" &&
		m.Aperture == "" && m.Shutter == "" && m.ISO == "" && m.TakenAt == ""
}

// Read extracts EXIF from path. Missing EXIF is not an error — returns empty Metadata.
// GPS coordinates are never included; HasGPS notes presence only.
func Read(path string) (Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err == nil {
		return fromGoExif(x), nil
	}

	// HEIC and some formats are not supported by goexif.
	ext := strings.ToLower(filepath.Ext(path))
	if (ext == ".heic" || ext == ".heif") && runtime.GOOS == "darwin" {
		if m, ok := fromMdls(path); ok {
			return m, nil
		}
	}
	return Metadata{}, nil
}

// CaptureTime returns the photo's capture timestamp when available.
func CaptureTime(path string) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err == nil {
		if dt, err := x.DateTime(); err == nil {
			return dt, true
		}
	}

	ext := strings.ToLower(filepath.Ext(path))
	if (ext == ".heic" || ext == ".heif") && runtime.GOOS == "darwin" {
		if t, ok := captureTimeMdls(path); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func fromGoExif(x *exif.Exif) Metadata {
	m := Metadata{}

	makeTag := tagString(x, exif.Make)
	model := tagString(x, exif.Model)
	m.Camera = joinCamera(makeTag, model)

	m.Lens = firstNonEmpty(
		tagString(x, exif.LensModel),
		tagString(x, exif.LensMake),
	)

	if fl, err := x.Get(exif.FocalLength); err == nil {
		if n, d, err := fl.Rat2(0); err == nil && d != 0 {
			mm := float64(n) / float64(d)
			m.FocalLength = formatFocal(mm)
		}
	}

	if fnum, err := x.Get(exif.FNumber); err == nil {
		if n, d, err := fnum.Rat2(0); err == nil && d != 0 {
			m.Aperture = formatAperture(float64(n) / float64(d))
		}
	}

	if exp, err := x.Get(exif.ExposureTime); err == nil {
		if n, d, err := exp.Rat2(0); err == nil && d != 0 {
			m.Shutter = formatShutter(float64(n) / float64(d))
		}
	}

	if iso, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if v, err := iso.Int(0); err == nil {
			m.ISO = fmt.Sprintf("ISO %d", v)
		}
	}

	if dt, err := x.DateTime(); err == nil {
		m.TakenAt = dt.Format("2006-01-02 15:04:05")
	}

	if _, _, err := x.LatLong(); err == nil {
		m.HasGPS = true
	}

	return m
}

func tagString(x *exif.Exif, field exif.FieldName) string {
	t, err := x.Get(field)
	if err != nil {
		return ""
	}
	if t.Format() == tiff.StringVal {
		s, err := t.StringVal()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(t.String())
}

func joinCamera(makeTag, model string) string {
	makeTag = strings.TrimSpace(makeTag)
	model = strings.TrimSpace(model)
	if makeTag == "" {
		return model
	}
	if model == "" {
		return makeTag
	}
	// Avoid "Sony Sony ILCE-6400"
	if strings.HasPrefix(strings.ToLower(model), strings.ToLower(makeTag)) {
		return model
	}
	return makeTag + " " + model
}

func formatFocal(mm float64) string {
	if mm <= 0 {
		return ""
	}
	if math.Abs(mm-math.Round(mm)) < 0.05 {
		return fmt.Sprintf("%.0fmm", math.Round(mm))
	}
	return fmt.Sprintf("%.1fmm", mm)
}

func formatAperture(f float64) string {
	if f <= 0 {
		return ""
	}
	if math.Abs(f-math.Round(f)) < 0.05 {
		return fmt.Sprintf("f/%.0f", math.Round(f))
	}
	return fmt.Sprintf("f/%.1f", f)
}

func formatShutter(sec float64) string {
	if sec <= 0 {
		return ""
	}
	if sec >= 1 {
		if math.Abs(sec-math.Round(sec)) < 0.05 {
			return fmt.Sprintf("%.0fs", math.Round(sec))
		}
		return fmt.Sprintf("%.1fs", sec)
	}
	denom := math.Round(1 / sec)
	if denom < 1 {
		denom = 1
	}
	return fmt.Sprintf("1/%.0fs", denom)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

var mdlsLine = regexp.MustCompile(`(?m)^kMDItem(\w+)\s*=\s*(.+)$`)

func fromMdls(path string) (Metadata, bool) {
	cmd := exec.Command("mdls",
		"-name", "kMDItemAcquisitionMake",
		"-name", "kMDItemAcquisitionModel",
		"-name", "kMDItemLensModel",
		"-name", "kMDItemFocalLength",
		"-name", "kMDItemFNumber",
		"-name", "kMDItemExposureTimeSeconds",
		"-name", "kMDItemISOSpeed",
		"-name", "kMDItemContentCreationDate",
		"-name", "kMDItemLatitude",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Metadata{}, false
	}

	vals := map[string]string{}
	for _, m := range mdlsLine.FindAllStringSubmatch(string(out), -1) {
		key, raw := m[1], strings.TrimSpace(m[2])
		if raw == "(null)" {
			continue
		}
		vals[key] = strings.Trim(raw, `"`)
	}
	if len(vals) == 0 {
		return Metadata{}, false
	}

	m := Metadata{
		Camera: joinCamera(vals["AcquisitionMake"], vals["AcquisitionModel"]),
		Lens:   vals["LensModel"],
	}
	if fl, err := strconv.ParseFloat(vals["FocalLength"], 64); err == nil {
		m.FocalLength = formatFocal(fl)
	}
	if f, err := strconv.ParseFloat(vals["FNumber"], 64); err == nil {
		m.Aperture = formatAperture(f)
	}
	if s, err := strconv.ParseFloat(vals["ExposureTimeSeconds"], 64); err == nil {
		m.Shutter = formatShutter(s)
	}
	if iso, err := strconv.Atoi(vals["ISOSpeed"]); err == nil {
		m.ISO = fmt.Sprintf("ISO %d", iso)
	}
	if t := vals["ContentCreationDate"]; t != "" {
		m.TakenAt = t
	}
	if _, ok := vals["Latitude"]; ok {
		m.HasGPS = true
	}
	return m, !m.Empty() || m.HasGPS
}

func captureTimeMdls(path string) (time.Time, bool) {
	cmd := exec.Command("mdls", "-name", "kMDItemContentCreationDate", "-name", "kMDItemDateCreated", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return time.Time{}, false
	}
	vals := map[string]string{}
	for _, m := range mdlsLine.FindAllStringSubmatch(string(out), -1) {
		key, raw := m[1], strings.TrimSpace(m[2])
		if raw == "(null)" {
			continue
		}
		vals[key] = strings.Trim(raw, `"`)
	}
	for _, key := range []string{"ContentCreationDate", "DateCreated"} {
		if s := vals[key]; s != "" {
			if t, err := time.Parse("2006-01-02 15:04:05 -0700", s); err == nil {
				return t, true
			}
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
