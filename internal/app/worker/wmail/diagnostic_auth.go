package wmail

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/diagnosticauth"
	"github.com/warmbly/warmbly/internal/repository"
)

func (w *WMail) verifyDiagnostic(ctx context.Context, msg *models.EmailMessageData) {
	if w.ExecutorID == uuid.Nil || w.EmailType == models.InboxProviderSMTPIMAP {
		return
	}
	authority, ok := w.SyncContext.(repository.DiagnosticAuthAuthority)
	if !ok {
		return
	}
	var token uuid.UUID
	for _, flag := range msg.Flags {
		if name, value, ok := strings.Cut(flag, ":"); ok && strings.EqualFold(strings.TrimSpace(name), config.WarmupVerifyHeader) {
			token, _ = uuid.Parse(strings.TrimSpace(value))
			break
		}
	}
	if token == uuid.Nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request := models.DiagnosticAuthRequest{Token: token, MailboxID: w.ID, WorkerID: w.ExecutorID, MessageID: msg.MessageID}
	grant, err := authority.DiagnosticAuth(ctx, request)
	if err != nil || grant == nil {
		return
	}
	var raw []byte
	switch w.EmailType {
	case models.InboxProviderGoogle:
		if w.GoogleData != nil && w.GoogleData.Client != nil {
			raw, err = w.GoogleData.Client.DiagnosticRawMessage(ctx, msg.GmailID, diagnosticauth.MaxMIMEBytes)
		}
	case models.InboxProviderOutlook:
		if w.GraphData != nil && w.GraphData.Client != nil {
			raw, err = w.GraphData.Client.DiagnosticRawMessage(ctx, msg.GmailID, diagnosticauth.MaxMIMEBytes)
		}
	}
	if err != nil || len(raw) == 0 {
		return
	}
	result := diagnosticauth.Verify(ctx, raw, *grant, nil)
	clear(raw)
	request.Nonce = grant.Nonce
	request.Result = &result
	_, _ = authority.DiagnosticAuth(ctx, request)
}
