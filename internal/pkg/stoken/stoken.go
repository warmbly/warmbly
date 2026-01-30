package stoken

import (
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
)

type Token struct {
	oauth2.TokenSource
	onUpdate func(*oauth2.Token) error
}

func New(token oauth2.TokenSource, onUpdate func(*oauth2.Token) error) *Token {
	return &Token{
		TokenSource: token,
		onUpdate:    onUpdate,
	}
}

func (s *Token) Token() (*oauth2.Token, error) {
	t, err := s.TokenSource.Token()
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if lastErr = s.onUpdate(t); lastErr != nil {
			delay := time.Duration(1<<attempt) * time.Second
			time.Sleep(delay)
			continue
		}
		lastErr = nil
		break
	}

	if lastErr != nil {
		log.Debug().Err(lastErr).Msg("Token Update")
	}
	return t, nil
}
