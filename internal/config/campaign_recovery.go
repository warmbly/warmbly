package config

import (
	"os"
	"strconv"
	"strings"
)

// CampaignSendIsolationEnabled is on unless explicitly disabled; every supported rollback release reads recovery pauses.
func CampaignSendIsolationEnabled() bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("CAMPAIGN_SEND_ISOLATION_ENABLED")))
	return err != nil || enabled
}
