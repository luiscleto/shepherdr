package uploads

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	DefaultDecodedLimit = int64(50 << 20)
	MetadataAllowance   = int64(1 << 20)
)

type Limit struct {
	DecodedBytes int64
	Unlimited    bool
}

func ParseLimit(value string) (Limit, error) {
	if value == "none" {
		return Limit{Unlimited: true}, nil
	}
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
		return Limit{}, errors.New("upload limit must be a positive byte count using optional KiB, MiB, or GiB, or none")
	}
	decoded := parsed * multiplier
	if decoded > (math.MaxInt64-MetadataAllowance)/4 {
		return Limit{}, fmt.Errorf("upload limit is too large for finite JSON accounting; use none")
	}
	return Limit{DecodedBytes: decoded}, nil
}

func EncodedContribution(decoded int64) int64 {
	if decoded <= 0 {
		return 0
	}
	return 4 * ((decoded + 2) / 3)
}

func (limit Limit) OuterBodyLimit() int64 {
	if limit.Unlimited {
		return 0
	}
	return 4*limit.DecodedBytes + MetadataAllowance
}

func (limit Limit) Validate(decodedTotal, encodedTotal, bodyBytes int64) error {
	if limit.Unlimited {
		return nil
	}
	if decodedTotal > limit.DecodedBytes {
		return errors.New("decoded files exceed the configured upload limit")
	}
	if bodyBytes > encodedTotal+MetadataAllowance {
		return errors.New("upload JSON and metadata exceed the fixed 1 MiB allowance")
	}
	return nil
}
