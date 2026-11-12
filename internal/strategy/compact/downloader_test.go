package compact

import (
	"errors"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestBuildPlanTasksPreservesSelectionOrderAndOffsets(t *testing.T) {
	d := NewDownloader("id", nil, nil, nil)
	plan := []types.ByteRange{
		{Start: 8, Length: 4},
		{Start: 0, Length: 2},
		{Start: 8, Length: 4},
	}
	// Chunk size larger than every window: one task per window.
	tasks := d.buildPlanTasks(plan, 1<<20)
	want := []types.Task{
		{Offset: 8, DstOffset: 0, Length: 4},
		{Offset: 0, DstOffset: 4, Length: 2},
		{Offset: 8, DstOffset: 6, Length: 4},
	}
	if len(tasks) != len(want) {
		t.Fatalf("got %d tasks %+v, want %d", len(tasks), tasks, len(want))
	}
	for i := range want {
		if tasks[i] != want[i] {
			t.Fatalf("task %d = %+v, want %+v", i, tasks[i], want[i])
		}
	}

	// The dense output space must be fully covered with no gaps.
	var covered int64
	for _, task := range tasks {
		if task.DstOffset != covered {
			t.Fatalf("gap or overlap at output offset %d, expected %d", task.DstOffset, covered)
		}
		covered += task.Length
	}
	if covered != 10 {
		t.Fatalf("covered %d output bytes, want 10", covered)
	}
}

func TestBuildPlanTasksSplitsWindowsWithoutCrossingBoundaries(t *testing.T) {
	d := NewDownloader("id", nil, nil, nil)
	plan := []types.ByteRange{{Start: 100, Length: 10}, {Start: 0, Length: 5}}
	tasks := d.buildPlanTasks(plan, 4) // 4-byte chunks
	want := []types.Task{
		{Offset: 100, DstOffset: 0, Length: 4},
		{Offset: 104, DstOffset: 4, Length: 4},
		{Offset: 108, DstOffset: 8, Length: 2},
		{Offset: 0, DstOffset: 10, Length: 4},
		{Offset: 4, DstOffset: 14, Length: 1},
	}
	if len(tasks) != len(want) {
		t.Fatalf("got %d tasks %+v, want %d", len(tasks), tasks, len(want))
	}
	for i := range want {
		if tasks[i] != want[i] {
			t.Fatalf("task %d = %+v, want %+v", i, tasks[i], want[i])
		}
	}
	// Every task keeps a constant source->destination delta inside its window.
	for _, task := range tasks {
		if task.Offset < 0 || task.DstOffset < 0 || task.Length <= 0 {
			t.Fatalf("invalid task %+v", task)
		}
	}
}

func TestParseContentRange(t *testing.T) {
	good := []struct {
		header            string
		start, end, total int64
	}{
		{"bytes 0-0/16", 0, 0, 16},
		{"bytes 8-11/16", 8, 11, 16},
		{"bytes 100-200/9999", 100, 200, 9999},
	}
	for _, tc := range good {
		start, end, total, err := parseContentRange(tc.header)
		if err != nil || start != tc.start || end != tc.end || total != tc.total {
			t.Fatalf("parseContentRange(%q) = %d-%d/%d, %v", tc.header, start, end, total, err)
		}
	}

	bad := []string{
		"",
		"bytes 0-0/*",
		"bytes 0-0",
		"0-0/16",
		"bytes a-0/16",
		"bytes 0-b/16",
		"bytes 0-0/x",
		"bytes 5-2/16",
	}
	for _, header := range bad {
		if _, _, _, err := parseContentRange(header); err == nil {
			t.Fatalf("parseContentRange(%q) succeeded, want error", header)
		}
	}
}

func TestRangeContractErrorIsDistinct(t *testing.T) {
	if errors.Is(ErrRangeContract, types.ErrPermanentHTTP) {
		t.Fatal("range contract error must not be classified as a generic HTTP error")
	}
}

func TestChunkSizeAlignment(t *testing.T) {
	d := NewDownloader("id", nil, nil, types.DefaultRuntimeConfig())
	chunk := d.chunkSize(10, 1)
	if chunk <= 0 || chunk%types.AlignSize != 0 {
		t.Fatalf("chunk size %d must be positive and aligned to %d", chunk, types.AlignSize)
	}
	large := d.chunkSize(64*1024*1024, 4)
	if large <= 0 || large%types.AlignSize != 0 {
		t.Fatalf("large chunk size %d must be positive and aligned to %d", large, types.AlignSize)
	}
}

func TestUnverifiedOutputTasksFromBitmap(t *testing.T) {
	chunkSize := int64(4)
	compactSize := int64(16) // four chunks
	// Only chunk index 1 (output bytes 4-7) is complete.
	bitmap := make([]byte, 1)
	bitmap[0] = byte(types.ChunkCompleted) << 2
	tasks := unverifiedOutputTasksFromBitmap(bitmap, compactSize, chunkSize)
	want := []types.Task{
		{Offset: 0, DstOffset: 0, Length: 4},
		{Offset: 8, DstOffset: 8, Length: 8},
	}
	if len(tasks) != len(want) {
		t.Fatalf("got %+v, want %+v", tasks, want)
	}
	for i := range want {
		if tasks[i] != want[i] {
			t.Fatalf("task %d = %+v, want %+v", i, tasks[i], want[i])
		}
	}
}

func TestAttachSourceOffsetsMapsThroughPlan(t *testing.T) {
	d := NewDownloader("id", nil, nil, nil)
	// Plan: src[8:12] -> dst[0:4], src[0:2] -> dst[4:6], src[8:12] -> dst[6:10].
	plan := []types.ByteRange{{Start: 8, Length: 4}, {Start: 0, Length: 2}, {Start: 8, Length: 4}}
	// One output run spanning the boundary between window 0 and window 1.
	outputTasks := []types.Task{{Offset: 2, DstOffset: 2, Length: 4}}
	got := d.attachSourceOffsets(plan, outputTasks)
	want := []types.Task{
		{Offset: 10, DstOffset: 2, Length: 2}, // window 0 tail
		{Offset: 0, DstOffset: 4, Length: 2},  // window 1 head
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("task %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
