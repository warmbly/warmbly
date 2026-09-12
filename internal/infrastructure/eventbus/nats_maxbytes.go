package eventbus

import (
	"fmt"
	"strconv"
	"strings"
)

// parseByteSize reads NATS_MAX_BYTES. Plain bytes, or a size with a suffix:
// 2GiB, 512MB, 1G. Binary and decimal are both accepted because operators
// reach for either, and a stream ceiling does not need the distinction to be
// load bearing.
//
// Empty means unset, which leaves the stream bounded only by MaxAge and the
// account's own quota.
func parseByteSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	upper := strings.ToUpper(s)

	// Longest suffix first: GIB has to match before GB, and GB before G.
	units := []struct {
		suffix string
		mult   int64
	}{
		{"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
		{"B", 1},
	}
	for _, u := range units {
		if !strings.HasSuffix(upper, u.suffix) {
			continue
		}
		num := strings.TrimSpace(upper[:len(upper)-len(u.suffix)])
		// Fractional sizes are the natural way to write 2.5GiB.
		f, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("eventbus nats: NATS_MAX_BYTES %q is not a size", raw)
		}
		if f < 0 {
			return 0, fmt.Errorf("eventbus nats: NATS_MAX_BYTES %q is negative", raw)
		}
		return int64(f * float64(u.mult)), nil
	}

	n, err := strconv.ParseInt(upper, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("eventbus nats: NATS_MAX_BYTES %q is not a size", raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("eventbus nats: NATS_MAX_BYTES %q is negative", raw)
	}
	return n, nil
}
