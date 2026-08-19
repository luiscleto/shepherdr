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

type ProtocolError struct {
	Got  uint32
	Want uint32
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("Herdr protocol %d is not supported; Shepherdr requires protocol %d", e.Got, e.Want)
}
