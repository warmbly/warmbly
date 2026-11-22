package nodelogs

import (
	"testing"

	"github.com/google/uuid"
)

func TestNodeLogsGenericRuntimeCoverageUnavailableWithoutCache(t *testing.T) {
	for _, service := range []*Service{nil, {}} {
		history, err := service.History(t.Context(), uuid.New(), Filter{})
		if err != nil || history.GenericErrorCoverage != "unavailable" || history.GenericErrorReason != "unhooked_runtime_sources" {
			t.Fatal("unhooked generic runtime error sources claimed as captured", err)
		}
	}
}
