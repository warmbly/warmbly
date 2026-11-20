package repository

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

// The recent-sync window spans the longest full check interval an operator may configure.
func TestProviderRestoredHoldWindowCoversTheFullCheckInterval(t *testing.T) {
	want := "interval '" + strconv.Itoa(int(config.SyncFullCheckRecent/time.Minute)) + " minutes'"
	if !strings.Contains(providerRestoredHold, want) {
		t.Fatalf("the recent-sync window is not %s", want)
	}
}
