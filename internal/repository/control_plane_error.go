package repository

import "fmt"

// ControlPlaneHTTPError exposes only status, never response bodies, to worker evidence.
type ControlPlaneHTTPError struct{ Status int }

func (e *ControlPlaneHTTPError) Error() string {
	return fmt.Sprintf("control-plane request failed: status %d", e.Status)
}
