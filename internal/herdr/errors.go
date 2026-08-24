package herdr

import "fmt"

type InvalidStatusError struct{ Status string }

func (e *InvalidStatusError) Error() string {
	return fmt.Sprintf("Herdr returned unsupported agent status %q", e.Status)
}

type IncoherentSnapshotError struct{ Detail string }

func (e *IncoherentSnapshotError) Error() string {
	return "Herdr returned an incoherent snapshot: " + e.Detail
}

type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Herdr API %s: %s", e.Code, e.Message)
}

type CompatibilityError struct {
	Version        string
	Protocol       uint32
	InvalidVersion bool
	Inconsistent   bool
}

func (e *CompatibilityError) Error() string {
	supportedRange := fmt.Sprintf("Shepherdr supports Herdr %s through %s", minimumSupportedVersion, maximumSupportedVersion)
	if e.InvalidVersion {
		return fmt.Sprintf("Herdr reported an invalid version %q; %s", e.Version, supportedRange)
	}
	if e.Inconsistent {
		return fmt.Sprintf("Herdr %s reported inconsistent connection details; %s", e.Version, supportedRange)
	}
	return fmt.Sprintf("Herdr %s is older than the supported Herdr range; %s", e.Version, supportedRange)
}
