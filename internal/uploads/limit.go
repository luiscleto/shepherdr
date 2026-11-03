package uploads

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

const (
	DefaultLimit      = int64(1 << 30)
	MetadataAllowance = int64(1 << 20)
)

// Limit is the file bytes one send may carry, or none.
type Limit struct {
	Bytes     int64
	Unlimited bool
}

func ParseLimit(value string) (Limit, error) {
	if value == "none" {
		return Limit{Unlimited: true}, nil
	}
	size, err := ParseSize(value)
	if err != nil {
		return Limit{}, errors.New("upload limit must be a positive byte count using optional KiB, MiB, or GiB, or none")
	}
	return Limit{Bytes: size}, nil
}

// ParseSize reads a positive byte count with an optional KiB, MiB, or GiB
// suffix.
func ParseSize(value string) (int64, error) {
	multiplier := int64(1)
	number := value
	for suffix, candidate := range map[string]int64{"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30} {
		if strings.HasSuffix(value, suffix) {
			multiplier = candidate
			number = strings.TrimSuffix(value, suffix)
			break
		}
	}
	parsed, err := strconv.ParseInt(number, 10, 64)
	if err != nil || parsed < 1 || parsed > math.MaxInt64/multiplier {
		return 0, errors.New("must be a positive byte count using optional KiB, MiB, or GiB")
	}
	return parsed * multiplier, nil
}

// Allows reports whether a send whose files total total bytes fits.
func (limit Limit) Allows(total int64) bool {
	return limit.Unlimited || total <= limit.Bytes
}
