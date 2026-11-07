package tasks

import (
	htmltemplate "html/template"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplvars"
)

// TemplateSender is the typed .Sender namespace; see tmplvars.Sender.
type TemplateSender = tmplvars.Sender

func templateContext(account *models.Email, unsubscribeURL string) TemplateContext {
	context := TemplateContext{UnsubscribeLink: unsubscribeURL}
	if account == nil {
		return context
	}
	first, last := splitSenderName(account.Name)
	context.Sender = TemplateSender{
		Name:                     strings.TrimSpace(account.Name),
		FirstName:                first,
		LastName:                 last,
		Email:                    account.SendFrom(),
		MailboxEmail:             account.Email,
		SendAsEmail:              strings.TrimSpace(account.SendAsEmail),
		ReplyTo:                  account.ReplyToHeader(),
		SignaturePlain:           account.SignaturePlain,
		SignatureHTML:            htmltemplate.HTML(strings.ReplaceAll(account.SignatureHTML, "\x00", "")),
		SignatureSync:            account.SignatureSync,
		SignatureCode:            account.SignatureCode,
		Provider:                 account.Provider,
		Status:                   account.Status,
		MailHost:                 account.MailHost,
		AuthMethod:               account.AuthMethod,
		Vendor:                   account.Vendor,
		AvatarURL:                account.AvatarURL,
		Tags:                     append([]string(nil), account.Tags...),
		Timezone:                 account.ClockTimezone(),
		CampaignLimit:            account.CampaignLimit,
		MinWaitTime:              account.MinWaitTime,
		SaveToSent:               account.SaveToSent,
		RelayFolderMoves:         account.RelayFolderMoves,
		TrackingDomain:           account.TrackingDomain,
		TrackingDomainVerified:   account.TrackingDomainVerified,
		TrackingDomainVerifiedAt: templateTime(account.TrackingDomainVerifiedAt),
		TrackDirectMail:          account.TrackDirectMail,
		AuthState:                account.AuthState,
		AuthSPF:                  account.AuthSPF,
		AuthDKIM:                 account.AuthDKIM,
		AuthDMARC:                account.AuthDMARC,
		AuthDMARCPolicy:          account.AuthDMARCPolicy,
		AuthReason:               account.AuthReason,
		AuthCheckedAt:            templateTime(account.AuthCheckedAt),
		AuthFailingSince:         templateTime(account.AuthFailingSince),
		Warmup:                   templateTime(account.Warmup),
		WarmupPausedAt:           templateTime(account.WarmupPausedAt),
		WarmupBase:               account.WarmupBase,
		WarmupMax:                account.WarmupMax,
		WarmupIncrease:           account.WarmupIncrease,
		WarmupReplyRate:          account.WarmupReplyRate,
		WarmupTag:                account.WarmupTag,
		WarmupPoolType:           account.WarmupPoolType,
		WarmupStartTime:          account.WarmupStartTime,
		WarmupEndTime:            account.WarmupEndTime,
		WarmupDays:               account.WarmupDays,
		WarmupPlacement:          account.WarmupPlacement,
		WarmupFolder:             account.WarmupFolder,
		WarmupRetentionDays:      account.WarmupRetentionDays,
		LastSyncedAt:             templateTime(&account.LastSyncedAt),
		CreatedAt:                templateTime(&account.CreatedAt),
		UpdatedAt:                templateTime(&account.UpdatedAt),
	}
	return context
}

// splitSenderName returns the first word of name and the remaining words joined by single spaces.
func splitSenderName(name string) (first, last string) {
	words := strings.Fields(name)
	if len(words) == 0 {
		return "", ""
	}
	return words[0], strings.Join(words[1:], " ")
}

func templateTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
