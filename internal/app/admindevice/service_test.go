package admindevice

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

type deviceIssuer struct {
	count        atomic.Int32
	proof        time.Time
	source, user uuid.UUID
	mu           sync.Mutex
}

func (i *deviceIssuer) GenerateAdminDeviceSession(_ context.Context, source, user uuid.UUID, at time.Time) (*models.Token, *errx.Error) {
	i.count.Add(1)
	i.mu.Lock()
	i.proof, i.source, i.user = at, source, user
	i.mu.Unlock()
	return &models.Token{AccessToken: "test-access", RefreshToken: "test-refresh"}, nil
}

func deviceFixture(t *testing.T) (*Service, *deviceIssuer) {
	t.Helper()
	url := os.Getenv("WARMBLY_TEST_REDIS")
	if url == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(url)
	if err != nil {
		t.Fatal(err)
	}
	i := &deviceIssuer{}
	s := &Service{cache: c, issuer: i, prefix: "test-admin-device:" + uuid.NewString() + ":", instance: "https://backend.example.test"}
	t.Cleanup(func() {
		keys, _ := c.Keys(context.Background(), s.prefix+"*").Result()
		if len(keys) > 0 {
			_ = c.Del(context.Background(), keys...).Err()
		}
		_ = c.Close()
	})
	return s, i
}

func TestAdminDeviceRequiresExplicitBoundConsentAndIsOneUse(t *testing.T) {
	s, issuer := deviceFixture(t)
	ctx := t.Context()
	start, xerr := s.Start(ctx, Request{ClientName: "My diagnostic laptop"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(start.DeviceSecret) != 43 || strings.Contains(start.VerificationPath, start.DeviceSecret) {
		t.Fatal("unsafe secret delivery")
	}
	key := s.prefix + "grant:" + crypt.SHA256(start.DeviceSecret)
	if ttl := s.cache.PTTL(ctx, key).Val(); ttl <= 0 || ttl > TTL {
		t.Fatal("grant has no finite expiry", ttl)
	}
	if raw := s.cache.Get(ctx, key).Val(); strings.Contains(raw, start.DeviceSecret) {
		t.Fatal("raw secret persisted")
	}
	proof := time.Now().UTC().Add(-time.Minute)
	session := &models.Session{ID: uuid.New(), UserID: uuid.New(), MFAVerified: true, ReauthAt: &proof}
	description, xerr := s.Describe(ctx, start.UserCode, session)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r := s.cache.Get(ctx, key).Val(); !strings.Contains(r, `"status":"pending"`) {
		t.Fatal("opening description authorized the request")
	}
	other := *session
	other.ID = uuid.New()
	if xerr := s.Decide(ctx, start.UserCode, description.ConsentToken, "approved", &other); xerr == nil {
		t.Fatal("consent transferred to another session")
	}
	if xerr := s.Decide(ctx, start.UserCode, description.ConsentToken, "approved", session); xerr != nil {
		t.Fatal(xerr)
	}
	if xerr := s.Decide(ctx, start.UserCode, description.ConsentToken, "approved", session); xerr == nil {
		t.Fatal("approval replay accepted")
	}
	var wg sync.WaitGroup
	var delivered atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, xerr := s.Poll(ctx, start.DeviceSecret)
			if xerr == nil && res.Token != nil {
				delivered.Add(1)
			}
		}()
	}
	wg.Wait()
	if delivered.Load() != 1 || issuer.count.Load() != 1 {
		t.Fatal("parallel poll double issuance", delivered.Load(), issuer.count.Load())
	}
	if !issuer.proof.Equal(proof) || issuer.source != session.ID || issuer.user != session.UserID {
		t.Fatal("approval provenance lost")
	}
	if _, xerr := s.Poll(ctx, start.DeviceSecret); xerr != ErrExpired {
		t.Fatal("consumed request replay", xerr)
	}
}

func TestAdminDeviceDenialExpirySlowPollingAndBruteForce(t *testing.T) {
	s, issuer := deviceFixture(t)
	ctx := t.Context()
	start, xerr := s.Start(ctx, Request{ClientName: "CLI terminal"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if r, xerr := s.Poll(ctx, start.DeviceSecret); xerr != nil || r.Status != "pending" {
		t.Fatal(r, xerr)
	}
	if _, xerr := s.Poll(ctx, start.DeviceSecret); xerr != ErrSlow {
		t.Fatal("rapid poll accepted", xerr)
	}
	proof := time.Now().UTC()
	session := &models.Session{ID: uuid.New(), UserID: uuid.New(), MFAVerified: true, ReauthAt: &proof}
	d, xerr := s.Describe(ctx, start.UserCode, session)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if xerr := s.Decide(ctx, start.UserCode, d.ConsentToken, "denied", session); xerr != nil {
		t.Fatal(xerr)
	}
	key := s.prefix + "grant:" + crypt.SHA256(start.DeviceSecret)
	var r Record
	if err := s.cache.GetJSON(ctx, key, &r); err != nil {
		t.Fatal(err)
	}
	r.LastPoll = 0
	if err := s.cache.SetJSON(ctx, key, r, TTL); err != nil {
		t.Fatal(err)
	}
	if res, xerr := s.Poll(ctx, start.DeviceSecret); xerr != nil || res.Status != "denied" || issuer.count.Load() != 0 {
		t.Fatal("denial minted credentials", res, xerr)
	}
	_ = s.cache.PExpire(ctx, key, -time.Second).Err()
	if _, xerr := s.Poll(ctx, start.DeviceSecret); xerr != ErrExpired {
		t.Fatal(xerr)
	}
	for range 20 {
		_, _ = s.Describe(ctx, "AAAA-BBBB", session)
	}
	if _, xerr := s.Describe(ctx, "AAAA-BBBB", session); xerr != ErrRate {
		t.Fatal("code guessing unbounded", xerr)
	}
}

func TestAdminDeviceRejectsStaleOrSingleFactorApproval(t *testing.T) {
	s, _ := deviceFixture(t)
	proof := time.Now().Add(-6 * time.Minute)
	session := &models.Session{ID: uuid.New(), UserID: uuid.New(), MFAVerified: true, ReauthAt: &proof}
	if xerr := s.Decide(t.Context(), "AAAA-BBBB", strings.Repeat("a", 43), "approved", session); xerr == nil {
		t.Fatal("old source manufactured freshness")
	}
	proof = time.Now()
	session.MFAVerified = false
	if xerr := s.Decide(t.Context(), "AAAA-BBBB", strings.Repeat("a", 43), "approved", session); xerr == nil {
		t.Fatal("single factor manufactured MFA")
	}
}

func TestAdminDeviceConcurrentDecisionsHaveOnlyOneWinner(t *testing.T) {
	s, _ := deviceFixture(t)
	start, xerr := s.Start(t.Context(), Request{ClientName: "CLI terminal"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	proof := time.Now().UTC()
	session := &models.Session{ID: uuid.New(), UserID: uuid.New(), MFAVerified: true, ReauthAt: &proof}
	d, xerr := s.Describe(t.Context(), start.UserCode, session)
	if xerr != nil {
		t.Fatal(xerr)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			decision := "approved"
			if i%2 == 0 {
				decision = "denied"
			}
			if s.Decide(t.Context(), start.UserCode, d.ConsentToken, decision, session) == nil {
				winners.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("approval/denial race had multiple winners", winners.Load())
	}
}
