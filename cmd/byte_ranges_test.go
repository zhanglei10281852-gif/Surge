package cmd

import (
	"math"
	"testing"

	"github.com/SurgeDM/Surge/internal/types"
)

func TestParseByteRanges(t *testing.T) {
	t.Run("ordered windows with overlap and repeat", func(t *testing.T) {
		ranges, err := parseByteRanges("8:4,0:2,8:4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := []types.ByteRange{{Start: 8, Length: 4}, {Start: 0, Length: 2}, {Start: 8, Length: 4}}
		if len(ranges) != len(expected) {
			t.Fatalf("got %d ranges, want %d", len(ranges), len(expected))
		}
		for i := range expected {
			if ranges[i] != expected[i] {
				t.Fatalf("range %d = %+v, want %+v", i, ranges[i], expected[i])
			}
		}
		total, err := types.CompactByteRanges(ranges)
		if err != nil || total != 10 {
			t.Fatalf("compact total = %d, %v; want 10", total, err)
		}
	})

	t.Run("whitespace is tolerated", func(t *testing.T) {
		ranges, err := parseByteRanges(" 0 : 2 , 8:4 ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ranges) != 2 || ranges[0].Start != 0 || ranges[1].Start != 8 {
			t.Fatalf("unexpected ranges: %+v", ranges)
		}
	})

	t.Run("maximum int64 window", func(t *testing.T) {
		ranges, err := parseByteRanges("0:9223372036854775807")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ranges[0].Length != math.MaxInt64 {
			t.Fatalf("length = %d, want MaxInt64", ranges[0].Length)
		}
	})
}

func TestParseByteRangesRejects(t *testing.T) {
	cases := map[string]string{
		"empty spec":            "",
		"whitespace only":       "   ",
		"trailing comma":        "0:4,",
		"empty entry":           ",0:4",
		"missing colon":         "0",
		"missing length":        "0:",
		"missing start":         ":4",
		"too many colons":       "0:4:8",
		"non-numeric start":     "a:4",
		"non-numeric length":    "0:b",
		"negative start":        "-1:4",
		"zero length":           "0:0",
		"negative length":       "0:-4",
		"length over int64":     "0:9223372036854775808",
		"start over int64":      "9223372036854775808:1",
		"sum overflows int64":   "0:9223372036854775807,0:1",
		"many windows overflow": "0:9000000000000000000,0:9000000000000000000",
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseByteRanges(spec); err == nil {
				t.Fatalf("parseByteRanges(%q) succeeded, want error", spec)
			}
		})
	}
}

func TestCompactByteRangesRepeatsCountIndependently(t *testing.T) {
	ranges := []types.ByteRange{{Start: 8, Length: 4}, {Start: 8, Length: 4}, {Start: 8, Length: 4}}
	total, err := types.CompactByteRanges(ranges)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 12 {
		t.Fatalf("total = %d, want 12 (each occurrence counts)", total)
	}
}
