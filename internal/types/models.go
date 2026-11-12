package types

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// Task represents a byte range to download.
type Task struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
	// DstOffset is the write position in the local output file. It is zero
	// for whole-file downloads, where Offset and DstOffset coincide. Compact
	// byte-range fetches carry an explicit destination offset so overlapping
	// or repeated source windows keep independent output positions.
	DstOffset int64 `json:"dst_offset,omitempty"`
}

// GobEncode preserves the historical 16-byte layout for whole-file tasks and
// appends DstOffset as a third little-endian uint64. GobDecode accepts both.
func (t *Task) GobEncode() ([]byte, error) {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint64(b[0:8], uint64(t.Offset))
	binary.LittleEndian.PutUint64(b[8:16], uint64(t.Length))
	binary.LittleEndian.PutUint64(b[16:24], uint64(t.DstOffset))
	return b, nil
}

func (t *Task) GobDecode(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) < 16 {
		return fmt.Errorf("corrupt task data: expected at least 16 bytes, got %d", len(data))
	}
	t.Offset = int64(binary.LittleEndian.Uint64(data[0:8]))
	t.Length = int64(binary.LittleEndian.Uint64(data[8:16]))
	if len(data) >= 24 {
		t.DstOffset = int64(binary.LittleEndian.Uint64(data[16:24]))
	}
	return nil
}

// ByteRange selects one source window for a compact byte-range fetch. Start
// is a zero-based source byte offset and Length is the number of bytes to copy.
type ByteRange struct {
	Start  int64 `json:"start"`
	Length int64 `json:"length"`
}

// CompactByteRanges returns the total compact output length of the selected
// windows, rejecting sums that overflow a signed 64-bit integer. Each window
// occurrence contributes its full Length, including repeated selections.
func CompactByteRanges(ranges []ByteRange) (int64, error) {
	var total int64
	for _, r := range ranges {
		if r.Length <= 0 {
			return 0, fmt.Errorf("byte range length must be positive: %d:%d", r.Start, r.Length)
		}
		if r.Start < 0 {
			return 0, fmt.Errorf("byte range start must not be negative: %d:%d", r.Start, r.Length)
		}
		if total > math.MaxInt64-r.Length {
			return 0, fmt.Errorf("compact output length overflows int64 for byte range %d:%d", r.Start, r.Length)
		}
		total += r.Length
	}
	return total, nil
}

// DownloadRecord is the canonical representation of a download's static configuration,
// persistent state, and runtime options. It replaces DownloadState, DownloadEntry, and DownloadConfig.
type DownloadRecord struct {
	Artifact            ArtifactPolicy `json:"artifact,omitzero"`
	ErrorCode           string         `json:"error_code,omitempty"`
	CategoryMoveError   string         `json:"category_move_error,omitempty"`
	PendingCategoryMove string         `json:"pending_category_move,omitempty"`
	CategoryAssigned    bool           `json:"category_assigned,omitempty"`
	CategoryOverride    *string        `json:"category_override,omitempty"`
	// Identity & Core Info
	ID         string `json:"id"`
	URLHash    string `json:"url_hash"`
	URL        string `json:"url"`
	Filename   string `json:"filename"`
	OutputPath string `json:"output_path"`
	DestPath   string `json:"dest_path"`
	TotalSize  int64  `json:"total_size"`
	Downloaded int64  `json:"downloaded"`

	// ByteRanges non-empty selects compact byte-range fetch mode: TotalSize is
	// then the compact output length and SourceSize is the confirmed source
	// length used to validate every partial response.
	ByteRanges []ByteRange `json:"byte_ranges,omitempty"`
	SourceSize int64       `json:"source_size,omitempty"`

	// Status
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`

	// Lifecycle Timestamps & Stats
	CreatedAt   int64   `json:"created_at"`
	PausedAt    int64   `json:"paused_at,omitempty"`
	CompletedAt int64   `json:"completed_at,omitempty"`
	TimeTaken   int64   `json:"time_taken,omitempty"`
	Elapsed     int64   `json:"elapsed,omitempty"`
	AvgSpeed    float64 `json:"avg_speed,omitempty"`

	// Resume State (Persistent)
	Tasks           []Task `json:"tasks,omitempty"`
	ChunkBitmap     []byte `json:"chunk_bitmap,omitempty"`
	ActualChunkSize int64  `json:"actual_chunk_size,omitempty"`
	FileHash        string `json:"file_hash,omitempty"`

	// Configuration Options (Persistent)
	Mirrors      []string `json:"mirrors,omitempty"`
	RateLimit    int64    `json:"rate_limit,omitempty"`
	RateLimitSet bool     `json:"rate_limit_set,omitempty"`
	Workers      int      `json:"workers,omitempty"`
	MinChunkSize int64    `json:"min_chunk_size,omitempty"`

	// Execution & Throttle State (Transient across retries)
	ThrottleEpisodeStart time.Time `json:"-"`

	// Runtime / Transient Configuration. The store controls persistence with
	// explicit projections because encoding/gob ignores struct tags. Headers are
	// retained only in per-download detail state for authenticated resumes.
	IsResume           bool                 `json:"-"`
	ProgressCh         chan<- DownloadEvent `json:"-"`
	ProgressState      interface{}          `json:"-"` // typically *progress.DownloadProgress
	Runtime            *RuntimeConfig       `json:"-"`
	Headers            map[string]string    `json:"-"`
	Limiter            ByteLimiter          `json:"-"`
	IsExplicitCategory bool                 `json:"-"`
	SupportsRange      bool                 `json:"-"`
}

// MasterList holds all tracked downloads.
type MasterList struct {
	Downloads []DownloadRecord `json:"downloads"`
}

// DownloadStatus is the transient view returned to the TUI and API clients.
type DownloadStatus struct {
	Phase            string         `json:"phase,omitempty"`
	MaxConnections   int            `json:"max_connections,omitempty"`
	Artifact         ArtifactPolicy `json:"artifact,omitzero"`
	ErrorCode        string         `json:"error_code,omitempty"`
	Moving           bool           `json:"moving,omitempty"`
	FileMissing      bool           `json:"file_missing,omitempty"`
	CategoryOverride *string        `json:"category_override,omitempty"`
	ID               string         `json:"id"`
	URL              string         `json:"url"`
	Filename         string         `json:"filename"`
	DestPath         string         `json:"dest_path,omitempty"`
	TotalSize        int64          `json:"total_size"`
	Downloaded       int64          `json:"downloaded"`
	Progress         float64        `json:"progress"`
	Speed            float64        `json:"speed"`
	Status           string         `json:"status"`
	Error            string         `json:"error,omitempty"`
	ETA              int64          `json:"eta"`
	Connections      int            `json:"connections"`
	AddedAt          int64          `json:"added_at"`
	TimeTaken        int64          `json:"time_taken"`
	AvgSpeed         float64        `json:"avg_speed"`
	RateLimit        int64          `json:"rate_limit,omitempty"`
	RateLimitSet     bool           `json:"rate_limit_set,omitempty"`
}

// CancelResult carries enough metadata for callers to emit lifecycle events
// without creating an import cycle back to the worker pool.
type CancelResult struct {
	Found     bool
	Filename  string
	DestPath  string
	Completed bool
	WasQueued bool
}

type MirrorStatus struct {
	URL    string
	Active bool
	Error  bool
}

// ChunkStatus represents the status of a visualization chunk
type ChunkStatus int

const (
	ChunkPending     ChunkStatus = 0 // 00
	ChunkDownloading ChunkStatus = 1 // 01
	ChunkCompleted   ChunkStatus = 2 // 10 (Bit 2 set)
)
