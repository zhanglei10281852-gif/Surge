package types

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestTaskGobRoundTripIncludesDstOffset(t *testing.T) {
	original := Task{Offset: 100, Length: 24, DstOffset: 16}
	data, err := original.GobEncode()
	if err != nil {
		t.Fatalf("GobEncode: %v", err)
	}
	if len(data) != 24 {
		t.Fatalf("encoded length = %d, want 24", len(data))
	}
	var decoded Task
	if err := decoded.GobDecode(data); err != nil {
		t.Fatalf("GobDecode: %v", err)
	}
	if decoded != original {
		t.Fatalf("round trip = %+v, want %+v", decoded, original)
	}
}

func TestTaskGobDecodeLegacy16ByteLayout(t *testing.T) {
	// Checkpoints written before DstOffset existed carry exactly 16 bytes.
	legacy := make([]byte, 16)
	binary.LittleEndian.PutUint64(legacy[0:8], 42)
	binary.LittleEndian.PutUint64(legacy[8:16], 7)

	var task Task
	if err := task.GobDecode(legacy); err != nil {
		t.Fatalf("GobDecode legacy: %v", err)
	}
	if task.Offset != 42 || task.Length != 7 || task.DstOffset != 0 {
		t.Fatalf("legacy decode = %+v, want Offset=42 Length=7 DstOffset=0", task)
	}
}

func TestCompactByteRangesValidation(t *testing.T) {
	if total, err := CompactByteRanges(nil); err != nil || total != 0 {
		t.Fatalf("empty ranges = %d, %v; want 0, nil", total, err)
	}
	if _, err := CompactByteRanges([]ByteRange{{Start: -1, Length: 1}}); err == nil {
		t.Fatal("negative start accepted")
	}
	if _, err := CompactByteRanges([]ByteRange{{Start: 0, Length: 0}}); err == nil {
		t.Fatal("zero length accepted")
	}
	if _, err := CompactByteRanges([]ByteRange{{Start: 0, Length: math.MaxInt64}, {Start: 0, Length: 1}}); err == nil {
		t.Fatal("overflowing sum accepted")
	}
	total, err := CompactByteRanges([]ByteRange{{Start: 8, Length: 4}, {Start: 0, Length: 2}, {Start: 8, Length: 4}})
	if err != nil || total != 10 {
		t.Fatalf("compact total = %d, %v; want 10", total, err)
	}
}
