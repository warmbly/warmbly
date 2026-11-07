package wmail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

// GmailFullMailScope is the consent that lets a Google token sign in over IMAP.
const GmailFullMailScope = "https://mail.google.com/"

// gmailIMAPIdle is how long the engagement session stays open after its last batch.
const gmailIMAPIdle = 90 * time.Second

// ErrGmailIMAPScope is a token that carries no full mail access, by Google's own listing or the control plane's refusal.
var ErrGmailIMAPScope = errors.New("gmail: no token with " + GmailFullMailScope)

// GmailIMAPSession is the slice of the IMAP client warmup engagement uses on Gmail.
type GmailIMAPSession interface {
	AllFolders(ctx context.Context) ([]models.Mailbox, error)
	FindUIDInFolder(ctx context.Context, mailboxName, rfcMessageID string) (uint32, error)
	MoveToFolder(ctx context.Context, sourceMailbox, dstFolder string, uid uint32) (bool, error)
	CopyToFolder(ctx context.Context, sourceMailbox, dstFolder string, uid uint32) error
	MarkAsRead(ctx context.Context, mailboxName string, uid uint32) error
	MarkImportant(ctx context.Context, mailboxName string, uid uint32) error
	Close() error
}

// GmailIMAP is a Google mailbox's one IMAP session for warmup engagement, opened on demand and closed when idle.
type GmailIMAP struct {
	// Dial opens a signed-in session.
	Dial func(ctx context.Context) (GmailIMAPSession, error)

	mu      sync.Mutex
	session GmailIMAPSession
	gen     uint64
	refused atomic.Pointer[gmailIMAPRefusal]
}

// gmailIMAPRetryAfter is how long a refusal the worker saw itself holds before IMAP is tried again, so a repaired grant takes effect.
const gmailIMAPRetryAfter = 6 * time.Hour

type gmailIMAPRefusal struct {
	reason string
	at     time.Time
	// standing is the control plane's answer, which a republished mailbox replaces.
	standing bool
}

// NewGmailIMAP dials imap.gmail.com with the mailbox's own OAuth token source.
func NewGmailIMAP(lifetime context.Context, email string, tokens oauth2.TokenSource) *GmailIMAP {
	return &GmailIMAP{Dial: func(context.Context) (GmailIMAPSession, error) {
		tok, err := tokens.Token()
		if err != nil {
			// The control plane refusing an IMAP token (a delegation entry without the scope) is a standing answer.
			var refused *repository.BrokeredTokenRefused
			if errors.As(err, &refused) {
				return nil, fmt.Errorf("%w: %s", ErrGmailIMAPScope, refused.Message)
			}
			return nil, err
		}
		// A refreshed token lists what was granted; one restored from storage says nothing and is tried.
		if granted, _ := tok.Extra("scope").(string); strings.TrimSpace(granted) != "" && !strings.Contains(" "+granted+" ", " "+GmailFullMailScope+" ") {
			return nil, ErrGmailIMAPScope
		}
		c := &imap.Client{
			Context:  lifetime,
			Email:    email,
			AuthType: models.AuthOAuth2,
			Oauth2:   &models.Oauth2Service{Host: imap.GmailHost, Port: imap.GmailPort, Token: tokens},
		}
		if merr := c.Connect(); merr != nil {
			_ = c.Close()
			return nil, merr
		}
		return c, nil
	}}
}

// Refused is why this mailbox's engagement runs through the Gmail API, or "" while IMAP is in use.
func (g *GmailIMAP) Refused() string {
	r := g.refused.Load()
	if r == nil {
		return ""
	}
	if !r.standing && time.Since(r.at) > gmailIMAPRetryAfter {
		g.refused.CompareAndSwap(r, nil)
		return ""
	}
	return r.reason
}

// Refuse records that IMAP cannot serve this mailbox; true only for the first call.
func (g *GmailIMAP) Refuse(reason string) bool {
	return g.refused.CompareAndSwap(nil, &gmailIMAPRefusal{reason: reason, at: time.Now()})
}

// RefuseStanding records the control plane's refusal, which holds for the life of this mailbox.
func (g *GmailIMAP) RefuseStanding(reason string) {
	g.refused.Store(&gmailIMAPRefusal{reason: reason, at: time.Now(), standing: true})
}

// With runs fn on the mailbox's session, dialing it first when none is open; batches never overlap.
func (g *GmailIMAP) With(ctx context.Context, fn func(GmailIMAPSession) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gen++
	if g.session == nil {
		s, err := g.Dial(ctx)
		if err != nil {
			return err
		}
		g.session = s
	}
	err := fn(g.session)
	if g.Refused() != "" {
		g.dropLocked()
		return err
	}
	gen := g.gen
	time.AfterFunc(gmailIMAPIdle, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.gen == gen {
			g.dropLocked()
		}
	})
	return err
}

func (g *GmailIMAP) dropLocked() {
	if g.session != nil {
		_ = g.session.Close()
		g.session = nil
	}
}
