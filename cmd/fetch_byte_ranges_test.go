package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SurgeDM/Surge/internal/testutil"
	"github.com/SurgeDM/Surge/internal/types"
)

// rangeTestServer implements a strict range server with configurable failure
// modes for compact byte-range fetch integration tests.
type rangeTestServer struct {
	*httptest.Server
	payload []byte

	rangeRequests atomic.Int64
	fullRequests  atomic.Int64

	mu                sync.Mutex
	ignoreRangeOnData bool // 200 instead of 206 for non-probe GETs
	badContentRange   bool // 206 with a wrong total for non-probe GETs
	earlyEOFOnce      bool // cut the first data response short
	blockUntilRelease bool // hold data responses until releaseCh
	releaseCh         chan struct{}
	releaseOnce       sync.Once
}

func newRangeTestServer(t *testing.T, payload []byte) *rangeTestServer {
	t.Helper()
	s := &rangeTestServer{payload: payload, releaseCh: make(chan struct{})}
	s.Server = testutil.NewHTTPServerT(t, http.HandlerFunc(s.handle))
	return s
}

func (s *rangeTestServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(s.payload)))
		w.WriteHeader(http.StatusOK)
		return
	}
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		s.fullRequests.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(s.payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.payload)
		return
	}
	s.rangeRequests.Add(1)
	start, end, ok := parseTestRange(rangeHeader, int64(len(s.payload)))
	if !ok {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	isProbe := start == 0 && end == 0

	s.mu.Lock()
	ignore := s.ignoreRangeOnData
	badRange := s.badContentRange
	cutEarly := s.earlyEOFOnce
	block := s.blockUntilRelease
	if s.earlyEOFOnce {
		s.earlyEOFOnce = false
	}
	s.mu.Unlock()

	if !isProbe && ignore {
		s.fullRequests.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(s.payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.payload)
		return
	}

	total := int64(len(s.payload))
	if !isProbe && badRange {
		total = int64(len(s.payload)) + 1
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)

	if !isProbe && block {
		// Write the first slab, flush, then park until the test releases us so
		// a timeout can checkpoint a partially completed window.
		first := end - start + 1
		if first > 4096 {
			first = 4096
		}
		_, _ = w.Write(s.payload[start : start+first])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-s.releaseCh:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(s.payload[start+first : end+1])
		return
	}

	if !isProbe && cutEarly {
		cut := end - start + 1
		if cut > 64 {
			cut = 64
		}
		_, _ = w.Write(s.payload[start : start+cut])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return // early EOF: client must retry the remainder
	}
	_, _ = w.Write(s.payload[start : end+1])
}

func (s *rangeTestServer) release() {
	s.releaseOnce.Do(func() { close(s.releaseCh) })
}

func parseTestRange(header string, size int64) (start, end int64, ok bool) {
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.Split(spec, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if parts[1] == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, false
		}
	}
	if start < 0 || end >= size || start > end {
		return 0, 0, false
	}
	return start, end, true
}

func compactExpectation(payload []byte, ranges []types.ByteRange) []byte {
	var out bytes.Buffer
	for _, r := range ranges {
		out.Write(payload[r.Start : r.Start+r.Length])
	}
	return out.Bytes()
}

func TestFetchByteRangesCompactContentAndOrder(t *testing.T) {
	setupIsolatedCmdState(t)
	resetCommandConnectionState(t)
	payload := []byte("0123456789ABCDEF") // 16 source bytes
	srv := newRangeTestServer(t, payload)
	directory := t.TempDir()

	code, result, _ := executeFetchTest(t, context.Background(),
		"--output", directory, "--filename", "compact.bin",
		"--byte-ranges", "8:4,0:2,8:4", srv.URL+"/file")
	if code != 0 || !result.OK || len(result.Items) != 1 {
		t.Fatalf("fetch failed: code=%d result=%+v", code, result)
	}
	item := result.Items[0]
	if item.Artifact == nil || item.Artifact.SizeBytes != "10" {
		t.Fatalf("artifact receipt = %+v, want 10 compact bytes", item.Artifact)
	}
	if item.DownloadedBytes != "10" || item.TotalBytes == nil || *item.TotalBytes != "10" {
		t.Fatalf("progress totals not compact-based: downloaded=%q total=%v", item.DownloadedBytes, item.TotalBytes)
	}
	want := compactExpectation(payload, []types.ByteRange{{Start: 8, Length: 4}, {Start: 0, Length: 2}, {Start: 8, Length: 4}})
	data, err := os.ReadFile(filepath.Join(directory, "compact.bin"))
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("compact content = %q, want %q (err=%v)", data, want, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "compact.bin.surge")); !os.IsNotExist(err) {
		t.Fatal("working file was not removed after publication")
	}
	if srv.fullRequests.Load() != 0 {
		t.Fatalf("server observed %d non-range requests; only selected windows may be fetched", srv.fullRequests.Load())
	}
	if srv.rangeRequests.Load() < 2 {
		t.Fatalf("expected ranged probe plus data requests, got %d range requests", srv.rangeRequests.Load())
	}
}

func TestFetchByteRangesArgumentValidation(t *testing.T) {
	setupIsolatedCmdState(t)
	resetCommandConnectionState(t)
	payload := []byte("0123456789ABCDEF")
	srv := newRangeTestServer(t, payload)
	second := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(payload)
		}
	}))
	defer second.Close()

	cases := []struct {
		name string
		args []string
	}{
		{"empty list", []string{"--byte-ranges", "", srv.URL}},
		{"malformed token", []string{"--byte-ranges", "x:3", srv.URL}},
		{"negative start", []string{"--byte-ranges", "-1:2", srv.URL}},
		{"zero length", []string{"--byte-ranges", "0:0", srv.URL}},
		{"negative length", []string{"--byte-ranges", "0:-2", srv.URL}},
		{"numeric overflow", []string{"--byte-ranges", "0:9223372036854775808", srv.URL}},
		{"sum overflow", []string{"--byte-ranges", "9223372036854775807:1,0:1", srv.URL}},
		{"multiple urls", []string{"--byte-ranges", "0:1", srv.URL, second.URL}},
		{"mirror list", []string{"--byte-ranges", "0:1", srv.URL + "," + second.URL}},
		{"resume conflict", []string{"--byte-ranges", "0:1", "--resume", "12345678-1234-1234-1234-123456789012", srv.URL}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			args := append([]string{"--output", directory, "--filename", "reject.bin"}, tc.args...)
			code, result, _ := executeFetchTest(t, context.Background(), args...)
			if code != 2 || result.OK {
				t.Fatalf("expected invalid-arguments exit 2, got code=%d result=%+v", code, result)
			}
			entries, _ := os.ReadDir(directory)
			if len(entries) != 0 {
				t.Fatalf("rejected fetch left files behind: %v", entries)
			}
		})
	}
}

func TestFetchByteRangesWindowExceedsSourceLeavesNoWork(t *testing.T) {
	setupIsolatedCmdState(t)
	resetCommandConnectionState(t)
	payload := []byte("0123456789ABCDEF") // 16 bytes
	srv := newRangeTestServer(t, payload)
	directory := t.TempDir()

	code, result, _ := executeFetchTest(t, context.Background(),
		"--output", directory, "--filename", "too-far.bin",
		"--byte-ranges", "0:4,12:8", srv.URL+"/file")
	if code == 0 || result.OK {
		t.Fatalf("out-of-bounds window accepted: %+v", result)
	}
	if result.Items[0].Artifact != nil {
		t.Fatal("rejected window produced an artifact receipt")
	}
	for _, name := range []string{"too-far.bin", "too-far.bin.surge"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("rejected window left %s behind", name)
		}
	}
}

func TestFetchByteRangesRejects200AndBadContentRange(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789ABCDEF"), 256) // 4 KiB

	t.Run("server ignores range on data requests", func(t *testing.T) {
		setupIsolatedCmdState(t)
		resetCommandConnectionState(t)
		srv := newRangeTestServer(t, payload)
		srv.mu.Lock()
		srv.ignoreRangeOnData = true
		srv.mu.Unlock()
		directory := t.TempDir()

		code, result, _ := executeFetchTest(t, context.Background(),
			"--output", directory, "--filename", "two-hundred.bin",
			"--byte-ranges", "0:16,8:16", srv.URL+"/file")
		if code == 0 || result.OK || result.Items[0].Artifact != nil {
			t.Fatalf("200 response was accepted: code=%d %+v", code, result)
		}
		if _, err := os.Stat(filepath.Join(directory, "two-hundred.bin")); !os.IsNotExist(err) {
			t.Fatal("unpublished 200 failure promoted an artifact")
		}
		if _, err := os.Stat(filepath.Join(directory, "two-hundred.bin.surge")); os.IsNotExist(err) {
			t.Fatal("working file should be retained for an unpublished failure")
		}
	})

	t.Run("content-range total does not match source", func(t *testing.T) {
		setupIsolatedCmdState(t)
		resetCommandConnectionState(t)
		srv := newRangeTestServer(t, payload)
		srv.mu.Lock()
		srv.badContentRange = true
		srv.mu.Unlock()
		directory := t.TempDir()

		code, result, _ := executeFetchTest(t, context.Background(),
			"--output", directory, "--filename", "bad-range.bin",
			"--byte-ranges", "0:16", srv.URL+"/file")
		if code == 0 || result.OK || result.Items[0].Artifact != nil {
			t.Fatalf("mismatched Content-Range accepted: code=%d %+v", code, result)
		}
		if _, err := os.Stat(filepath.Join(directory, "bad-range.bin")); !os.IsNotExist(err) {
			t.Fatal("unpublished Content-Range failure promoted an artifact")
		}
	})
}

func TestFetchByteRangesEarlyEOFRetriesWithinWindow(t *testing.T) {
	setupIsolatedCmdState(t)
	resetCommandConnectionState(t)
	payload := bytes.Repeat([]byte("0123456789ABCDEF"), 1024) // 16 KiB
	srv := newRangeTestServer(t, payload)
	srv.mu.Lock()
	srv.earlyEOFOnce = true
	srv.mu.Unlock()
	directory := t.TempDir()

	ranges := []types.ByteRange{{Start: 16, Length: 4096}, {Start: 0, Length: 32}, {Start: 4096, Length: 4096}}
	code, result, _ := executeFetchTest(t, context.Background(),
		"--output", directory, "--filename", "retried.bin",
		"--byte-ranges", "16:4096,0:32,4096:4096", srv.URL+"/file")
	if code != 0 || !result.OK {
		t.Fatalf("retry after early EOF failed: code=%d %+v", code, result)
	}
	want := compactExpectation(payload, ranges)
	data, err := os.ReadFile(filepath.Join(directory, "retried.bin"))
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("retried compact content mismatch: %v", err)
	}
	if result.Items[0].DownloadedBytes != strconv.Itoa(len(want)) {
		t.Fatalf("retried output double-counted bytes: %q want %d", result.Items[0].DownloadedBytes, len(want))
	}
}

func TestFetchByteRangesCheckpointAndResume(t *testing.T) {
	setupIsolatedCmdState(t)
	resetCommandConnectionState(t)
	payload := bytes.Repeat([]byte("0123456789ABCDEF"), 8*1024) // 128 KiB
	ranges := []types.ByteRange{{Start: 1024, Length: 32 * 1024}, {Start: 0, Length: 1024}, {Start: 64 * 1024, Length: 32 * 1024}}
	compactSize := 32*1024 + 1024 + 32*1024
	want := compactExpectation(payload, ranges)

	srv := newRangeTestServer(t, payload)
	srv.mu.Lock()
	srv.blockUntilRelease = true
	srv.mu.Unlock()
	directory := t.TempDir()

	first, firstResult, _ := func() (int, fetchResult, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return executeFetchTest(t, ctx,
			"--output", directory, "--filename", "paused.bin",
			"--timeout", "800ms",
			"--byte-ranges", "1024:32768,0:1024,65536:32768", srv.URL+"/file")
	}()
	if firstResult.CheckpointConfirmed == nil || !*firstResult.CheckpointConfirmed || firstResult.SessionID == "" {
		t.Fatalf("timeout did not checkpoint compact fetch: code=%d %+v", first, firstResult)
	}
	if firstResult.Items[0].TotalBytes == nil || *firstResult.Items[0].TotalBytes != strconv.Itoa(compactSize) {
		t.Fatalf("checkpoint total not compact size: %+v", firstResult.Items[0])
	}
	srv.release()

	// Resume in a fresh process: no --byte-ranges, only the session ID.
	code, resumed, _ := executeFetchTest(t, context.Background(), "--resume", firstResult.SessionID)
	if code != 0 || !resumed.OK || resumed.Items[0].Artifact == nil {
		t.Fatalf("compact resume failed: code=%d %+v", code, resumed)
	}
	if resumed.Items[0].Artifact.SizeBytes != strconv.Itoa(compactSize) {
		t.Fatalf("resumed artifact size = %s, want %d", resumed.Items[0].Artifact.SizeBytes, compactSize)
	}
	data, err := os.ReadFile(filepath.Join(directory, "paused.bin"))
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("resumed compact content mismatch: %v", err)
	}
	if srv.fullRequests.Load() != 0 {
		t.Fatalf("resume issued non-range requests: %d", srv.fullRequests.Load())
	}
}
