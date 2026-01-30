package goog

import "golang.org/x/oauth2"

type savingTokenSource struct {
	oauth2.TokenSource
	save func(*oauth2.Token)
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.TokenSource.Token()
	if err != nil {
		return nil, err
	}
	s.save(t)
	return t, nil
}
