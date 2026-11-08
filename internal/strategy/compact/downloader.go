// Package compact implements the compact byte-range fetch strategy.
//
// A compact download copies several source windows into one dense output file
// in the caller's selection order. Windows may be unordered, overlapping or
// repeated; every occurrence occupies its own output position. Each task
// therefore carries both a source offset (used for the HTTP Range request)
// and a destination offset (used for the local write and progress tracking).
//
// The strategy is strictly range-based: a server that ignores Range, or a
// 206 response whose Content-Range does not match the requested interval or
// the confirmed source length, is a terminal contract error. There is no
// whole-file fallback and no trim-after-download path.
package compact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SurgeDM/Surge/internal/progress"
	"github.com/SurgeDM/Surge/internal/store"
	"github.com/SurgeDM/Surge/internal/strategy/concurrent"
	"github.com/SurgeDM/Surge/internal/transport"
	"github.com/SurgeDM/Surge/internal/types"
	"github.com/SurgeDM/Surge/internal/utils"
)

// ErrRangeContract is returned when a partial response cannot be trusted:
// either the server ignored Range (200), or a 206 Content-Range does not
// match the requested interval / confirmed source length. It is terminal so
// the scheduler never burns retries against a broken contract.
var ErrRangeContract = errors.New("byte-range response contract violated")

// activeRange tracks the in-flight task cursors so a pause snapshot can
// reconstruct the unfinished part of a window.
type activeRange struct {
	mu        sync.Mutex
	srcOffset int64
	dstOffset int64
	remaining int64
}

func (a *activeRange) snapshot() types.Task {
	a.mu.Lock()
	defer a.mu.Unlock()
	return types.Task{Offset: a.srcOffset, DstOffset: a.dstOffset, Length: a.remaining}
}

func (a *activeRange) advance(n int64) {
	a.mu.Lock()
	a.srcOffset += n
	a.dstOffset += n
	a.remaining -= n
	a.mu.Unlock()
}

// Downloader runs compact byte-range transfers with the shared concurrency
// configuration and retry knobs carried by RuntimeConfig.
type Downloader struct {
	ID           string
	ProgressChan chan<- types.DownloadEvent
	State        *progress.DownloadProgress
	Runtime      *types.RuntimeConfig
	Limiter      types.ByteLimiter
	Headers      map[string]string

	activeMu sync.Mutex
	active   map[int]*activeRange
}

// NewDownloader constructs a compact-strategy downloader.
func NewDownloader(id string, progressCh chan<- types.DownloadEvent, state *progress.DownloadProgress, runtime *types.RuntimeConfig) *Downloader {
	if runtime == nil {
		runtime = types.DefaultRuntimeConfig()
	}
	return &Downloader{
		ID:           id,
		ProgressChan: progressCh,
		State:        state,
		Runtime:      runtime,
		active:       make(map[int]*activeRange),
	}
}

// Download executes the compact transfer. destPath is the final path; the
// reserved working file lives at destPath+IncompleteSuffix. savedTasks (from
// a pause checkpoint) drive resume; otherwise the plan windows are split.
func (d *Downloader) Download(ctx context.Context, rawurl, destPath string, plan []types.ByteRange, sourceSize, compactSize int64, savedTasks []types.Task, savedBitmap []byte, savedChunkSize int64, isResume bool) error {
	if len(plan) == 0 {
		return fmt.Errorf("compact download requires at least one byte range")
	}
	if sourceSize <= 0 || compactSize <= 0 {
		return fmt.Errorf("compact download requires confirmed source and output lengths")
	}
	if d.State != nil {
		d.State.SetURL(rawurl)
		d.State.SetDestPath(destPath)
		d.State.SetTotalSize(compactSize)
		d.State.SetCancelFunc(nil)
	}

	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if d.State != nil {
		d.State.SetCancelFunc(cancel)
	}

	httpTransport := transport.DefaultNetworkPool.AcquireTransport(d.Runtime.ProxyURL, d.Runtime.CustomDNS, 0)
	defer transport.DefaultNetworkPool.ReleaseTransport(httpTransport)
	client := &http.Client{Transport: httpTransport}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return types.ErrMaxRedirects
		}
		if len(via) > 0 {
			utils.CopyRedirectHeaders(req, via[0])
		}
		return nil
	}

	workingPath := destPath + types.IncompleteSuffix
	outFile, err := os.OpenFile(workingPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open working file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	effectiveSize := compactSize
	if isResume && d.State != nil {
		if downloaded := d.State.Bytes.VerifiedProgress.Load(); downloaded > 0 {
			effectiveSize = compactSize - downloaded
			if effectiveSize < 0 {
				effectiveSize = 0
			}
		}
	}
	numConns := concurrent.InitialConnectionCount(d.Runtime, effectiveSize)
	chunkSize := d.chunkSize(compactSize, numConns)

	if d.State != nil {
		d.State.InitBitmap(compactSize, chunkSize)
	}

	// Scheduler-level retries re-enter with a fresh config rather than an
	// explicit resume flag; adopt any durable compact checkpoint exactly like
	// the concurrent strategy does, so confirmed output bytes are never redone.
	if !isResume || len(savedTasks) == 0 {
		if saved, loadErr := store.LoadState(rawurl, destPath); loadErr == nil && saved != nil &&
			len(saved.ByteRanges) > 0 && len(saved.Tasks) > 0 && saved.TotalSize == compactSize {
			isResume = true
			savedTasks = saved.Tasks
			savedBitmap = saved.ChunkBitmap
			savedChunkSize = saved.ActualChunkSize
			if d.State != nil && saved.Downloaded > 0 {
				d.State.Bytes.Downloaded.Store(saved.Downloaded)
				d.State.Bytes.VerifiedProgress.Store(saved.Downloaded)
			}
		}
	}

	var tasks []types.Task
	if isResume && len(savedTasks) > 0 {
		tasks = append(tasks, savedTasks...)
		if d.State != nil && len(savedBitmap) > 0 && savedChunkSize > 0 {
			d.State.RestoreBitmap(savedBitmap, savedChunkSize)
		}
		d.restoreProgress(savedTasks)
	} else {
		if err := outFile.Truncate(compactSize); err != nil {
			return fmt.Errorf("failed to preallocate compact working file: %w", err)
		}
		tasks = d.buildPlanTasks(plan, chunkSize)
		if d.State != nil {
			d.State.Bytes.Downloaded.Store(0)
			d.State.SyncSessionStart()
		}
	}

	queue := concurrent.NewTaskQueue()
	queue.PushMultiple(tasks)

	// Mirrors the concurrent strategy's completion monitor: parked workers
	// block in Pop without context awareness, so completion or cancellation
	// must close the queue to release them.
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-downloadCtx.Done():
				queue.Close()
				return
			case <-ticker.C:
				if d.State != nil && d.State.Bytes.VerifiedProgress.Load() >= compactSize {
					queue.Close()
					return
				}
			}
		}
	}()

	var wg sync.WaitGroup
	workerErrors := make(chan error, numConns)
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			if err := d.worker(downloadCtx, workerID, rawurl, outFile, queue, sourceSize, compactSize, client); err != nil {
				if !errors.Is(err, context.Canceled) {
					workerErrors <- err
				}
				cancel()
			}
		}(i)
	}
	wg.Wait()
	close(workerErrors)

	var downloadErr error
	seen := make(map[string]bool)
	for err := range workerErrors {
		if err == nil || seen[err.Error()] {
			continue
		}
		seen[err.Error()] = true
		downloadErr = errors.Join(downloadErr, err)
	}

	if d.State != nil && d.State.IsPaused() {
		return d.saveSnapshot(destPath, rawurl, plan, sourceSize, compactSize, chunkSize, queue, true)
	}
	if downloadErr != nil {
		// Persist whatever remains even when peer cancellation joined the
		// error: a scheduler-level retry can then reuse confirmed output bytes.
		if saveErr := d.saveSnapshot(destPath, rawurl, plan, sourceSize, compactSize, chunkSize, queue, false); saveErr != nil {
			return errors.Join(downloadErr, fmt.Errorf("save resume state: %w", saveErr))
		}
		return downloadErr
	}
	if downloadCtx.Err() != nil {
		return downloadCtx.Err()
	}

	// A compact file is complete only when every output position is filled.
	if d.State != nil && d.State.Bytes.VerifiedProgress.Load() != compactSize {
		return fmt.Errorf("compact download finished with %d of %d output bytes", d.State.Bytes.VerifiedProgress.Load(), compactSize)
	}
	if err := outFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync compact working file: %w", err)
	}
	return nil
}

// chunkSize mirrors the concurrent strategy's chunk selection so existing
// worker and minimum-chunk configuration keeps applying.
func (d *Downloader) chunkSize(compactSize int64, numConns int) int64 {
	minChunk := d.Runtime.GetMinChunkSize()
	chunk := minChunk
	if !d.Runtime.SequentialDownload {
		chunk = compactSize / int64(numConns)
		if chunk < minChunk {
			chunk = minChunk
		}
	}
	chunk = (chunk / types.AlignSize) * types.AlignSize
	if chunk == 0 {
		chunk = types.AlignSize
	}
	return chunk
}

// buildPlanTasks splits each window into chunk-aligned tasks. Tasks never
// cross a window boundary, so every task has one constant source->destination
// offset delta.
func (d *Downloader) buildPlanTasks(plan []types.ByteRange, chunkSize int64) []types.Task {
	var tasks []types.Task
	var dstCursor int64
	for _, window := range plan {
		for pos := int64(0); pos < window.Length; pos += chunkSize {
			length := chunkSize
			if pos+length > window.Length {
				length = window.Length - pos
			}
			tasks = append(tasks, types.Task{
				Offset:    window.Start + pos,
				DstOffset: dstCursor + pos,
				Length:    length,
			})
		}
		dstCursor += window.Length
	}
	return tasks
}

// restoreProgress rebuilds verified output bytes from the checkpoint bitmap
// and the remaining dual-offset tasks.
func (d *Downloader) restoreProgress(remaining []types.Task) {
	if d.State == nil {
		return
	}
	bitmapTasks := make([]types.Task, len(remaining))
	for i, task := range remaining {
		// The bitmap is keyed by output positions, not source offsets.
		bitmapTasks[i] = types.Task{Offset: task.DstOffset, Length: task.Length}
	}
	d.State.RecalculateProgress(bitmapTasks)
	downloaded := d.State.Bytes.VerifiedProgress.Load()
	d.State.Bytes.Downloaded.Store(downloaded)
	d.State.SyncSessionStart()
}

func (d *Downloader) worker(ctx context.Context, id int, rawurl string, file *os.File, queue *concurrent.TaskQueue, sourceSize, compactSize int64, client *http.Client) error {
	buf := make([]byte, d.Runtime.GetWorkerBufferSize())
	maxRetries := d.Runtime.GetMaxTaskRetries()

	for {
		task, ok := queue.Pop()
		if !ok {
			return nil
		}
		if d.State != nil && d.State.Bytes.VerifiedProgress.Load() >= compactSize {
			return nil
		}
		if d.State != nil {
			d.State.ActiveWorkers.Add(1)
			d.State.UpdateChunkStatus(task.DstOffset, task.Length, types.ChunkDownloading)
		}

		active := &activeRange{srcOffset: task.Offset, dstOffset: task.DstOffset, remaining: task.Length}
		d.activeMu.Lock()
		d.active[id] = active
		d.activeMu.Unlock()

		var lastErr error
		attempts := 0
		for active.remaining > 0 {
			if ctx.Err() != nil {
				lastErr = ctx.Err()
				break
			}
			lastErr = d.fetchRange(ctx, rawurl, file, client, active, sourceSize, buf)
			if lastErr == nil {
				break
			}
			if errors.Is(lastErr, context.Canceled) || errors.Is(lastErr, context.DeadlineExceeded) {
				break
			}
			if errors.Is(lastErr, ErrRangeContract) || types.IsPermanentHTTPError(lastErr) {
				break
			}
			attempts++
			if attempts >= maxRetries {
				break
			}
			if !sleepWithContext(ctx, time.Duration(1<<attempts)*types.RetryBaseDelay) {
				lastErr = ctx.Err()
				break
			}
		}

		d.activeMu.Lock()
		delete(d.active, id)
		d.activeMu.Unlock()
		if d.State != nil {
			d.State.ActiveWorkers.Add(-1)
		}

		if d.State != nil && d.State.IsPaused() {
			if active.remaining > 0 {
				queue.Push(active.snapshot())
			}
			return nil
		}
		if ctx.Err() != nil {
			if active.remaining > 0 {
				queue.Push(active.snapshot())
			}
			return ctx.Err()
		}
		if lastErr != nil {
			// Keep the unfinished part of this window available for another
			// worker or a scheduler-level retry. Other windows are never
			// touched because each task carries its own destination offset.
			if active.remaining > 0 {
				queue.Push(active.snapshot())
			}
			return lastErr
		}
	}
}

// fetchRange performs one strictly-validated partial request and streams it
// to the task's destination offset. Cursors in active advance only after a
// successful write, so retries continue exactly where the transfer stopped
// without recounting or overwriting confirmed output bytes.
func (d *Downloader) fetchRange(ctx context.Context, rawurl string, file *os.File, client *http.Client, active *activeRange, sourceSize int64, buf []byte) error {
	requested := active.snapshot()
	reqSrc := requested.Offset
	reqLen := requested.Length

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}
	for key, val := range d.Headers {
		if !strings.EqualFold(key, "Range") && !strings.EqualFold(key, "Accept-Encoding") {
			req.Header.Set(key, val)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", d.Runtime.GetUserAgent())
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", reqSrc, reqSrc+reqLen-1))
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return fmt.Errorf("server ignored Range and returned 200 for bytes=%d-%d: %w", reqSrc, reqSrc+reqLen-1, ErrRangeContract)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("server returned %d for byte range bytes=%d-%d", resp.StatusCode, reqSrc, reqSrc+reqLen-1)
	}
	if resp.StatusCode != http.StatusPartialContent {
		if types.IsPermanentHTTPStatus(resp.StatusCode) {
			return fmt.Errorf("unexpected status %d for byte range bytes=%d-%d: %w", resp.StatusCode, reqSrc, reqSrc+reqLen-1, types.ErrPermanentHTTP)
		}
		return fmt.Errorf("unexpected status %d for byte range bytes=%d-%d", resp.StatusCode, reqSrc, reqSrc+reqLen-1)
	}

	rangeStart, rangeEnd, rangeTotal, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRangeContract, err)
	}
	if rangeStart != reqSrc || rangeEnd != reqSrc+reqLen-1 || rangeTotal != sourceSize {
		return fmt.Errorf("%w: Content-Range bytes=%d-%d/%d does not match request bytes=%d-%d and source length %d",
			ErrRangeContract, rangeStart, rangeEnd, rangeTotal, reqSrc, reqSrc+reqLen-1, sourceSize)
	}

	for active.remaining > 0 {
		readSize := int64(len(buf))
		if readSize > active.remaining {
			readSize = active.remaining
		}
		readSoFar, readErr := readFull(resp.Body, buf[:readSize])
		if readSoFar > 0 {
			if d.Limiter != nil {
				if err := d.Limiter.WaitN(ctx, int64(readSoFar)); err != nil {
					return err
				}
			}
			current := active.snapshot()
			if _, writeErr := file.WriteAt(buf[:readSoFar], current.DstOffset); writeErr != nil {
				return fmt.Errorf("write error: %w", writeErr)
			}
			if d.State != nil {
				d.State.UpdateChunkStatus(current.DstOffset, int64(readSoFar), types.ChunkCompleted)
				d.State.Bytes.Downloaded.Add(int64(readSoFar))
			}
			active.advance(int64(readSoFar))
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			if active.remaining > 0 {
				cursor := active.snapshot()
				return fmt.Errorf("early EOF: %d bytes remain starting at source %d output %d", cursor.Length, cursor.Offset, cursor.DstOffset)
			}
			return nil
		}
		return fmt.Errorf("read error: %w", readErr)
	}
	return nil
}

// saveSnapshot persists the remaining dual-offset tasks and the selection
// plan so a later process can resume without the original --byte-ranges input.
func (d *Downloader) saveSnapshot(destPath, rawurl string, plan []types.ByteRange, sourceSize, compactSize, chunkSize int64, queue *concurrent.TaskQueue, emitPauseEvent bool) error {
	d.activeMu.Lock()
	remaining := make([]types.Task, 0, len(d.active)+queue.Len())
	for _, active := range d.active {
		if task := active.snapshot(); task.Length > 0 {
			remaining = append(remaining, task)
		}
	}
	d.activeMu.Unlock()
	remaining = append(remaining, queue.DrainRemaining()...)

	var remainingBytes int64
	for _, task := range remaining {
		remainingBytes += task.Length
	}

	var bitmap []byte
	var actualChunkSize int64
	if d.State != nil {
		bitmap, _, actualChunkSize, _, _ = d.State.GetBitmapSnapshot(false)
	}

	// Defensive safety net: if no remaining task survived but the compact file
	// still has gaps, rebuild the unfinished output runs from the bitmap and
	// map them back to source offsets through the selection plan.
	if remainingBytes == 0 && d.State != nil && d.State.Bytes.VerifiedProgress.Load() < compactSize &&
		len(bitmap) > 0 && actualChunkSize > 0 {
		if rebuilt := unverifiedOutputTasksFromBitmap(bitmap, compactSize, actualChunkSize); len(rebuilt) > 0 {
			remaining = d.attachSourceOffsets(plan, rebuilt)
			remainingBytes = 0
			for _, task := range remaining {
				remainingBytes += task.Length
			}
		}
	}

	downloaded := compactSize - remainingBytes
	if d.State != nil {
		verified := d.State.Bytes.VerifiedProgress.Load()
		if remainingBytes > 0 && verified > downloaded {
			// Never report less than the bytes physically verified on disk.
			downloaded = verified
		}
		if remainingBytes == 0 && verified < downloaded {
			// No remaining tasks but gaps remain: trust the verified counter
			// rather than claiming a fully completed compact file.
			downloaded = verified
		}
	}

	var elapsed time.Duration
	if d.State != nil {
		elapsed = d.State.FinalizePauseSession(downloaded)
	}

	var rateLimit int64
	var rateLimitSet bool
	if d.State != nil {
		rateLimit, rateLimitSet = d.State.GetRateLimit()
	}

	snapshot := &types.DownloadRecord{
		URL:             rawurl,
		ID:              d.ID,
		DestPath:        destPath,
		Filename:        filepath.Base(destPath),
		TotalSize:       compactSize,
		Downloaded:      downloaded,
		Tasks:           remaining,
		ChunkBitmap:     bitmap,
		ActualChunkSize: actualChunkSizeOr(actualChunkSize, chunkSize),
		Elapsed:         elapsed.Nanoseconds(),
		ByteRanges:      append([]types.ByteRange(nil), plan...),
		SourceSize:      sourceSize,
		RateLimit:       rateLimit,
		RateLimitSet:    rateLimitSet,
		Workers:         d.Runtime.GetWorkers(),
		MinChunkSize:    d.Runtime.GetMinChunkSize(),
		Headers:         maps.Clone(d.Headers),
	}
	if d.State != nil {
		d.State.SetPendingResumeState(snapshot)
	}
	if err := store.SaveStateWithOptions(rawurl, destPath, snapshot, store.SaveStateOptions{SkipFileHash: true}); err != nil {
		return fmt.Errorf("save compact state snapshot: %w", err)
	}

	if emitPauseEvent && d.ProgressChan != nil {
		event := types.DownloadEvent{
			Type:         types.EventPaused,
			DownloadID:   d.ID,
			Filename:     snapshot.Filename,
			Downloaded:   downloaded,
			State:        snapshot,
			RateLimit:    rateLimit,
			RateLimitSet: rateLimitSet,
			Workers:      snapshot.Workers,
			MinChunkSize: snapshot.MinChunkSize,
		}
		select {
		case d.ProgressChan <- event:
			if d.State != nil {
				d.State.TakePendingResumeState()
			}
		default:
			utils.Debug("Compact pause event queue full; scheduler will deliver saved state")
		}
		return types.ErrPaused
	}
	return nil
}

func actualChunkSizeOr(saved, fallback int64) int64 {
	if saved > 0 {
		return saved
	}
	return fallback
}

// unverifiedOutputTasksFromBitmap rebuilds output-space runs whose bitmap
// chunks are not marked complete. Offsets are compact file positions, not
// source positions.
func unverifiedOutputTasksFromBitmap(bitmap []byte, compactSize, chunkSize int64) []types.Task {
	if compactSize <= 0 || chunkSize <= 0 {
		return nil
	}
	numChunks := (compactSize + chunkSize - 1) / chunkSize
	var tasks []types.Task
	var start int64 = -1
	for i := int64(0); i < numChunks; i++ {
		status := types.ChunkPending
		if byteIndex := i / 4; byteIndex < int64(len(bitmap)) {
			status = types.ChunkStatus((bitmap[byteIndex] >> ((i % 4) * 2)) & 3)
		}
		if status != types.ChunkCompleted {
			if start < 0 {
				start = i * chunkSize
			}
			continue
		}
		if start >= 0 {
			tasks = append(tasks, types.Task{Offset: start, DstOffset: start, Length: i*chunkSize - start})
			start = -1
		}
	}
	if start >= 0 {
		tasks = append(tasks, types.Task{Offset: start, DstOffset: start, Length: compactSize - start})
	}
	return tasks
}

// attachSourceOffsets converts output-space runs into dual-offset tasks by
// intersecting them with the selection plan, splitting at window boundaries so
// every produced task keeps one constant source->destination delta.
func (d *Downloader) attachSourceOffsets(plan []types.ByteRange, outputTasks []types.Task) []types.Task {
	var windowStarts = make([]int64, 0, len(plan))
	var cursor int64
	for _, window := range plan {
		windowStarts = append(windowStarts, cursor)
		cursor += window.Length
	}

	var result []types.Task
	for _, outputTask := range outputTasks {
		dstStart := outputTask.DstOffset
		dstEnd := dstStart + outputTask.Length
		for i, window := range plan {
			windowStart := windowStarts[i]
			windowEnd := windowStart + window.Length
			overlapStart := max64(dstStart, windowStart)
			overlapEnd := min64(dstEnd, windowEnd)
			if overlapStart >= overlapEnd {
				continue
			}
			result = append(result, types.Task{
				Offset:    window.Start + (overlapStart - windowStart),
				DstOffset: overlapStart,
				Length:    overlapEnd - overlapStart,
			})
		}
	}
	return result
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// parseContentRange parses "bytes start-end/total" and rejects unknown sizes.
func parseContentRange(value string) (start, end, total int64, err error) {
	if value == "" {
		return 0, 0, 0, fmt.Errorf("missing Content-Range header")
	}
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range header %q", value)
	}
	spec := strings.TrimPrefix(value, "bytes ")
	slash := strings.LastIndex(spec, "/")
	if slash < 0 || slash+1 >= len(spec) {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range header %q", value)
	}
	totalText := spec[slash+1:]
	if totalText == "*" {
		return 0, 0, 0, fmt.Errorf("Content-Range carries unknown source length %q", value)
	}
	total, err = strconv.ParseInt(totalText, 10, 64)
	if err != nil || total <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range total %q", totalText)
	}
	span := spec[:slash]
	dash := strings.Index(span, "-")
	if dash < 0 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range span %q", value)
	}
	start, err = strconv.ParseInt(span[:dash], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range start %q", span[:dash])
	}
	end, err = strconv.ParseInt(span[dash+1:], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range end %q", span[dash+1:])
	}
	if start < 0 || end < start {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range bounds %q", value)
	}
	return start, end, total, nil
}

// readFull reads until the buffer is filled or the body ends. io.EOF with a
// partial buffer is reported as io.ErrUnexpectedEOF so callers distinguish a
// truncated response from a clean boundary.
func readFull(body io.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := body.Read(buf[total:])
		total += n
		if err != nil {
			if errors.Is(err, io.EOF) && total > 0 && total < len(buf) {
				return total, io.ErrUnexpectedEOF
			}
			return total, err
		}
		if n == 0 {
			return total, io.ErrUnexpectedEOF
		}
	}
	return total, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
