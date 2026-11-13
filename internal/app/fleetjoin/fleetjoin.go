// Package fleetjoin keeps the fleet join token in AWS Parameter Store usable, so
// workers an Auto Scaling Group launches can always join. Optional: a deployment
// that does not set FLEET_JOIN_TOKEN_PARAMETER runs none of it.
package fleetjoin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
)

const (
	// RenewBefore is how close to expiry a stored token is replaced.
	RenewBefore = 10 * 24 * time.Hour
	checkEvery  = time.Hour
	retryEvery  = 5 * time.Minute
)

// Parameter is FLEET_JOIN_TOKEN_PARAMETER, the SecureString the fleet's workers read.
func Parameter() string {
	return strings.TrimSpace(os.Getenv("FLEET_JOIN_TOKEN_PARAMETER"))
}

// Store holds the plaintext token; Get answers "" for a parameter that does not exist.
type Store interface {
	Get(ctx context.Context) (string, error)
	Put(ctx context.Context, token string) error
}

// Issuer is the instance's join token: fleetnode.Service.
type Issuer interface {
	VerifyJoinToken(ctx context.Context, token string) error
	JoinTokenExpiresAt(ctx context.Context) (*time.Time, error)
	IssueJoinToken(ctx context.Context, ttl time.Duration) (string, time.Time, error)
}

// Sync leaves a stored token that still joins for more than RenewBefore, and
// otherwise issues a new one and stores it. Issuing revokes the previous token,
// so a failed store is retried by the next Sync rather than undone.
func Sync(ctx context.Context, store Store, issuer Issuer, now time.Time) (bool, error) {
	current, err := store.Get(ctx)
	if err != nil {
		return false, err
	}
	if current != "" && issuer.VerifyJoinToken(ctx, current) == nil {
		expiresAt, err := issuer.JoinTokenExpiresAt(ctx)
		if err != nil {
			return false, err
		}
		if expiresAt != nil && expiresAt.Sub(now) > RenewBefore {
			return false, nil
		}
	}
	token, _, err := issuer.IssueJoinToken(ctx, fleetnode.MaxJoinTokenTTL)
	if err != nil {
		return false, err
	}
	if err := store.Put(ctx, token); err != nil {
		return true, fmt.Errorf("store renewed join token: %w", err)
	}
	return true, nil
}

// Run keeps the parameter current until ctx ends. Run it on one backend at a
// time: two replicas issuing at once would each revoke the other's token.
func Run(ctx context.Context, issuer Issuer, parameter string) error {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("fleet join token: aws config: %w", err)
	}
	store := &parameterStore{client: ssm.NewFromConfig(cfg), name: parameter}
	for {
		wait := checkEvery
		renewed, err := Sync(ctx, store, issuer, time.Now())
		switch {
		case err != nil:
			log.Warn().Err(err).Str("parameter", parameter).Msg("fleet join token not renewed")
			wait = retryEvery
		case renewed:
			log.Info().Str("parameter", parameter).Msg("fleet join token renewed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

type parameterStore struct {
	client *ssm.Client
	name   string
}

func (p *parameterStore) Get(ctx context.Context) (string, error) {
	out, err := p.client.GetParameter(ctx, &ssm.GetParameterInput{Name: aws.String(p.name), WithDecryption: aws.Bool(true)})
	var missing *ssmtypes.ParameterNotFound
	if errors.As(err, &missing) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(aws.ToString(out.Parameter.Value)), nil
}

func (p *parameterStore) Put(ctx context.Context, token string) error {
	_, err := p.client.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(p.name),
		Value:     aws.String(token),
		Type:      ssmtypes.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
	})
	return err
}
