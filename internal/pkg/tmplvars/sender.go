package tmplvars

import htmltemplate "html/template"

// Sender is the .Sender namespace, an explicit allowlist: never embed the mailbox model or its methods.
type Sender struct {
	Name           string
	FirstName      string
	LastName       string
	Email          string
	MailboxEmail   string
	SendAsEmail    string
	ReplyTo        string
	SignaturePlain string
	// SignatureHTML is the mailbox's own markup, so an HTML body emits it as written.
	SignatureHTML            htmltemplate.HTML
	SignatureSync            bool
	SignatureCode            bool
	Provider                 string
	Status                   string
	MailHost                 string
	AuthMethod               string
	Vendor                   string
	AvatarURL                string
	Tags                     []string
	Timezone                 string
	CampaignLimit            int
	MinWaitTime              int
	SaveToSent               bool
	RelayFolderMoves         bool
	TrackingDomain           string
	TrackingDomainVerified   bool
	TrackingDomainVerifiedAt string
	TrackDirectMail          bool
	AuthState                string
	AuthSPF                  bool
	AuthDKIM                 bool
	AuthDMARC                bool
	AuthDMARCPolicy          string
	AuthReason               string
	AuthCheckedAt            string
	AuthFailingSince         string
	Warmup                   string
	WarmupPausedAt           string
	WarmupBase               int
	WarmupMax                int
	WarmupIncrease           int
	WarmupReplyRate          int
	WarmupTag                string
	WarmupPoolType           string
	WarmupStartTime          string
	WarmupEndTime            string
	WarmupDays               int
	WarmupPlacement          string
	WarmupFolder             string
	WarmupRetentionDays      int
	LastSyncedAt             string
	CreatedAt                string
	UpdatedAt                string
}
