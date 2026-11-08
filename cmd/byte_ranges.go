package cmd

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/SurgeDM/Surge/internal/types"
)

// parseByteRanges parses a --byte-ranges specification: a comma-separated
// list of start:length windows. start is a zero-based source byte offset and
// length is a positive byte count. Both are decimal signed 64-bit integers,
// so ParseInt rejects malformed tokens and out-of-range values. The summed
// output length is checked for int64 overflow up front.
func parseByteRanges(spec string) ([]types.ByteRange, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return nil, fmt.Errorf("--byte-ranges requires a non-empty comma-separated start:length list")
	}
	parts := strings.Split(trimmed, ",")
	ranges := make([]types.ByteRange, 0, len(parts))
	var total int64
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			return nil, fmt.Errorf("empty entry in --byte-ranges: %q", spec)
		}
		fields := strings.Split(item, ":")
		if len(fields) != 2 || strings.TrimSpace(fields[0]) == "" || strings.TrimSpace(fields[1]) == "" {
			return nil, fmt.Errorf("each --byte-ranges entry must be start:length: %q", item)
		}
		startText := strings.TrimSpace(fields[0])
		lengthText := strings.TrimSpace(fields[1])
		start, err := strconv.ParseInt(startText, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid byte-range start %q: %w", startText, err)
		}
		length, err := strconv.ParseInt(lengthText, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid byte-range length %q: %w", lengthText, err)
		}
		if start < 0 {
			return nil, fmt.Errorf("byte-range start must not be negative: %d:%d", start, length)
		}
		if length <= 0 {
			return nil, fmt.Errorf("byte-range length must be positive: %d:%d", start, length)
		}
		if total > math.MaxInt64-length {
			return nil, fmt.Errorf("compact output length overflows int64 at byte range %d:%d", start, length)
		}
		total += length
		ranges = append(ranges, types.ByteRange{Start: start, Length: length})
	}
	return ranges, nil
}
