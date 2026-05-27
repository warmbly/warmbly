package config

const (
	DefaultColor = "#c4c8cf"
	Domain       = "warmbly.com"
	LimitMin     = 10
	LimitMax     = 200

	CampaignLimitDefault  = 50
	MinWaitTimeDefault    = 600
	WarmupBaseDefault     = 10
	WarmupMaxDefault      = 40
	WarmupIncreaseDefault = 1

	MaxContactSize   = 10240
	MaxEmailBodySize = 200 * 1024 // 200 KB
	MaxEmailFolders  = 30

	// Sequences
	SequenceDefaultName  = "New Sequence"
	SequenceSubjectLimit = 100
	SequenceBodyLimit    = 30_000

	// Unibox
	UniboxLimitMin     = 1
	UniboxLimitMax     = 100
	UniboxLimitDefault = 50

	// WarmupVerifyHeader is the custom header carrying the warmup
	// verification token on outbound warmup mail. The name is intentionally
	// generic (not "X-Warmbly-*") so anti-spam vendors cannot trivially
	// cluster on the header name to fingerprint warmup traffic.
	WarmupVerifyHeader = "X-Mailtrace-Verify"
)
