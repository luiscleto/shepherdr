package uploads

import (
	"math"
	"testing"
)

func TestFiniteLimitCountsFileBytes(t *testing.T) {
	limit, err := ParseLimit("3")
	if err != nil {
		t.Fatal(err)
	}
	if !limit.Allows(3) || limit.Allows(4) {
		t.Fatalf("limit 3 allows 3: %t, allows 4: %t", limit.Allows(3), limit.Allows(4))
	}
}

func TestNoLimitAllowsAnySize(t *testing.T) {
	limit, err := ParseLimit("none")
	if err != nil || !limit.Unlimited || !limit.Allows(math.MaxInt64) {
		t.Fatalf("none = %+v, %v", limit, err)
	}
}

func TestDefaultLimitIsOneGiB(t *testing.T) {
	limit, err := ParseLimit("1GiB")
	if err != nil || limit.Bytes != DefaultLimit || DefaultLimit != 1<<30 {
		t.Fatalf("default text = %+v, %v", limit, err)
	}
}

func TestParseSizeRefusesBadValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1.5MiB", "1MB", "none", "MiB", "9223372036854775807GiB", " 1KiB"} {
		if size, err := ParseSize(value); err == nil {
			t.Fatalf("ParseSize(%q) = %d, want an error", value, size)
		}
	}
	for value, want := range map[string]int64{"1": 1, "8KiB": 8 << 10, "64MiB": 64 << 20, "2GiB": 2 << 30} {
		if size, err := ParseSize(value); err != nil || size != want {
			t.Fatalf("ParseSize(%q) = %d, %v, want %d", value, size, err, want)
		}
	}
}
