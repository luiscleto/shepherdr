package uploads

import "testing"

func TestFiniteLimitCountsEveryFilesBase64PaddingAndFixedMetadata(t *testing.T) {
	limit, err := ParseLimit("3")
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodedContribution(1) + EncodedContribution(2)
	if encoded != 8 {
		t.Fatalf("separate encoded contributions = %d, want 8", encoded)
	}
	if err := limit.Validate(3, encoded, encoded+MetadataAllowance); err != nil {
		t.Fatalf("exact finite bound failed: %v", err)
	}
	if err := limit.Validate(4, encoded, encoded); err == nil {
		t.Fatal("decoded overage was accepted")
	}
	if err := limit.Validate(3, encoded, encoded+MetadataAllowance+1); err == nil {
		t.Fatal("metadata overage was accepted")
	}
}

func TestNoLimitRemovesDecodedAndBodyBounds(t *testing.T) {
	limit, err := ParseLimit("none")
	if err != nil || !limit.Unlimited || limit.OuterBodyLimit() != 0 {
		t.Fatalf("none = %+v, %v", limit, err)
	}
	if err := limit.Validate(1<<62, 1<<62, 1<<62); err != nil {
		t.Fatalf("none retained a route bound: %v", err)
	}
}

func TestDefaultLimitIsFiftyMiB(t *testing.T) {
	limit, err := ParseLimit("50MiB")
	if err != nil || limit.DecodedBytes != DefaultDecodedLimit {
		t.Fatalf("default text = %+v, %v", limit, err)
	}
}
