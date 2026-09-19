package models

// Bucket is a physical photo location under the working directory.
type Bucket string

const (
	BucketRoot     Bucket = "root"
	BucketLiked    Bucket = "liked"
	BucketReview   Bucket = "review"
	BucketDisliked Bucket = "disliked"
)

// Valid reports whether b is a known bucket.
func (b Bucket) Valid() bool {
	switch b {
	case BucketRoot, BucketLiked, BucketReview, BucketDisliked:
		return true
	default:
		return false
	}
}

// LibraryBucket reports whether b can appear in the Library sidebar.
// Includes root (Unclassified) so burst groups can be browsed before culling.
func (b Bucket) LibraryBucket() bool {
	return b.Valid()
}

// Photo represents a photo file.
type Photo struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	BurstID    string `json:"burst_id,omitempty"`
	BurstIndex int    `json:"burst_index"`
	BurstSize  int    `json:"burst_size,omitempty"`
	TakenAt    string `json:"taken_at,omitempty"`
}

// PhotoAction is a classification destination (subset of buckets).
type PhotoAction string

const (
	ActionLiked    PhotoAction = "liked"
	ActionDisliked PhotoAction = "disliked"
	ActionReview   PhotoAction = "review"
)

// Valid reports whether a is a known action.
func (a PhotoAction) Valid() bool {
	switch a {
	case ActionLiked, ActionDisliked, ActionReview:
		return true
	default:
		return false
	}
}

// ToBucket maps a cull action to a bucket.
func (a PhotoAction) ToBucket() Bucket {
	return Bucket(a)
}

// ActionRequest is the body for POST /api/photos/:filename/action.
type ActionRequest struct {
	Action PhotoAction `json:"action"`
}

// BurstKeepRequest keeps one frame and classifies the rest of its burst.
type BurstKeepRequest struct {
	KeepAction PhotoAction `json:"keep_action"`
	RestAction PhotoAction `json:"rest_action"`
	From       Bucket      `json:"from"`
}

// BurstKeepResponse is returned after a burst keep operation.
type BurstKeepResponse struct {
	Success bool     `json:"success"`
	Kept    string   `json:"kept"`
	Moved   []string `json:"moved"`
	Stats   Stats    `json:"stats"`
}

// ActionRecord stores enough information to undo a move.
type ActionRecord struct {
	Filename     string
	OriginalPath string
	Destination  string
	Action       PhotoAction
	FromBucket   Bucket
	ToBucket     Bucket
}

// UndoEntry is one undo step; may contain multiple records for a bulk move.
type UndoEntry struct {
	Records []ActionRecord
}

// PhotosResponse is the GET /api/photos payload.
type PhotosResponse struct {
	Photos []Photo `json:"photos"`
	Stats  Stats   `json:"stats"`
}

// LibraryResponse is GET /api/library/:bucket.
type LibraryResponse struct {
	Bucket  Bucket       `json:"bucket"`
	Photos  []Photo      `json:"photos"`
	Bursts  []BurstGroup `json:"bursts"`
	Singles []Photo      `json:"singles"`
	Stats   *Stats       `json:"stats,omitempty"`
}

// BurstGroup is a cluster of photos detected by capture time.
type BurstGroup struct {
	ID     string  `json:"id"`
	Size   int     `json:"size"`
	Photos []Photo `json:"photos"`
}

// Stats reflects filesystem classification counts.
type Stats struct {
	Total     int `json:"total"`
	Remaining int `json:"remaining"` // unclassified (root); kept for Cull UI
	Root      int `json:"root"`
	Liked     int `json:"liked"`
	Disliked  int `json:"disliked"`
	Review    int `json:"review"`
}

// SessionStats tracks actions in the current process only.
type SessionStats struct {
	Reviewed           int     `json:"reviewed"`
	Liked              int     `json:"liked"`
	Disliked           int     `json:"disliked"`
	Review             int     `json:"review"`
	AvgDecisionSeconds float64 `json:"avg_decision_seconds"`
}

// StatsResponse combines filesystem and session stats.
type StatsResponse struct {
	Filesystem Stats        `json:"filesystem"`
	Session    SessionStats `json:"session"`
}

// ActionResponse is returned after a successful classification.
type ActionResponse struct {
	Success bool        `json:"success"`
	Photo   string      `json:"photo"`
	Action  PhotoAction `json:"action"`
	Stats   Stats       `json:"stats"`
}

// MoveRequest is POST /api/photos/move.
type MoveRequest struct {
	Filename string `json:"filename"`
	From     Bucket `json:"from"`
	To       Bucket `json:"to"`
}

// MoveResponse is a single move result.
type MoveResponse struct {
	Success  bool   `json:"success"`
	Filename string `json:"filename"`
	From     Bucket `json:"from,omitempty"`
	To       Bucket `json:"to,omitempty"`
	Stats    Stats  `json:"stats,omitempty"`
}

// BulkMoveItem is one photo in a bulk move.
type BulkMoveItem struct {
	Filename string `json:"filename"`
	From     Bucket `json:"from"`
}

// BulkMoveRequest is POST /api/photos/move-bulk.
type BulkMoveRequest struct {
	Photos []BulkMoveItem `json:"photos"`
	To     Bucket         `json:"to"`
}

// BulkMoveItemResult is per-file outcome.
type BulkMoveItemResult struct {
	Filename string `json:"filename"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// BulkMoveResponse summarizes a bulk move.
type BulkMoveResponse struct {
	Success     bool                 `json:"success"`
	MovedCount  int                  `json:"movedCount"`
	FailedCount int                  `json:"failedCount"`
	Results     []BulkMoveItemResult `json:"results"`
	Stats       Stats                `json:"stats"`
}

// ResetUnclassifiedResponse is POST /api/photos/reset-unclassified.
type ResetUnclassifiedResponse struct {
	Success     bool  `json:"success"`
	MovedCount  int   `json:"movedCount"`
	FailedCount int   `json:"failedCount"`
	Stats       Stats `json:"stats"`
}

// UndoPhoto is a file restored by undo.
type UndoPhoto struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Bucket Bucket `json:"bucket"`
}

// UndoResponse is returned after undo.
type UndoResponse struct {
	Success bool        `json:"success"`
	Photo   string      `json:"photo,omitempty"`
	Photos  []UndoPhoto `json:"photos,omitempty"`
	Count   int         `json:"count,omitempty"`
	Reason  string      `json:"reason,omitempty"`
	Stats   Stats       `json:"stats,omitempty"`
}

// ExportLikedRequest is POST /api/export/liked.
type ExportLikedRequest struct {
	Destination string `json:"destination"`
	BatchSize   int    `json:"batch_size,omitempty"`
	MaxBytes    int64  `json:"max_bytes,omitempty"`
}

// ExportLikedResponse is returned after exporting liked photos.
type ExportLikedResponse struct {
	Success     bool     `json:"success"`
	Destination string   `json:"destination"`
	BatchSize   int      `json:"batch_size"`
	MaxBytes    int64    `json:"max_bytes"`
	Exported    int      `json:"exported"`
	Failed      int      `json:"failed"`
	Batches     int      `json:"batches"`
	BatchDirs   []string `json:"batch_dirs"`
	Errors      []string `json:"errors,omitempty"`
}

// ErrorBody is the nested error object in API failures.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the consistent API error envelope.
type ErrorResponse struct {
	Success bool      `json:"success"`
	Error   ErrorBody `json:"error"`
	Reason  string    `json:"reason,omitempty"`
}

// ExifMetadata is display EXIF for a photo (no GPS coordinates).
type ExifMetadata struct {
	Camera      string `json:"camera,omitempty"`
	Lens        string `json:"lens,omitempty"`
	FocalLength string `json:"focal_length,omitempty"`
	Aperture    string `json:"aperture,omitempty"`
	Shutter     string `json:"shutter,omitempty"`
	ISO         string `json:"iso,omitempty"`
	TakenAt     string `json:"taken_at,omitempty"`
	HasGPS      bool   `json:"has_gps"`
}

// SessionState is lightweight resume metadata in .sorta/session.json.
type SessionState struct {
	Mode       string `json:"mode"`
	LastPhoto  string `json:"lastPhoto"`
	LastBucket string `json:"lastBucket"`
}
