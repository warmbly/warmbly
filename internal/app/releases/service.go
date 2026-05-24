// Package releases drives worker auto-update based on GitHub Releases.
//
// All configuration is env-driven so this is safe to ship in self-hosted
// deployments. A self-hoster can:
//   - point the poller at their own fork (RELEASES_GITHUB_REPO)
//   - point the image at their own registry (RELEASES_WORKER_IMAGE_REPO)
//   - disable the feature entirely (RELEASES_ENABLED=false)
//
// The service is push-driven, not poll-driven: a single check runs on
// backend boot to sync state, and from then on the GitHub webhook
// (POST /webhooks/github/releases) triggers checks. There is no recurring
// timer.
package releases

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker_orchestrator"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type Config struct {
	Enabled         bool   // RELEASES_ENABLED (default true)
	GithubRepo      string // "owner/repo", e.g. "warmbly/warmbly"
	WorkerImageRepo string // "ghcr.io/warmbly/warmbly/worker"
	WebhookSecret   string // shared secret for GitHub webhook HMAC
	GithubToken     string // optional, raises API rate limit
	HTTPClient      *http.Client
}

type Service struct {
	cfg          Config
	credsRepo    repository.CredentialsRepository
	workerRepo   repository.WorkerRepository
	orchestrator *worker_orchestrator.Orchestrator
	http         *http.Client

	// In-memory cache of the latest resolved versions per channel.
	// Populated on every CheckGitHub call; surfaced via GetState() to UI.
	stateMu sync.Mutex
	state   State
}

type State struct {
	LastCheckedAt time.Time              `json:"last_checked_at"`
	LastError     string                 `json:"last_error,omitempty"`
	Channels      map[string]ChannelView `json:"channels"`
	GithubRepo    string                 `json:"github_repo"`
	ImageRepo     string                 `json:"image_repo"`
	Enabled       bool                   `json:"enabled"`
}

type ChannelView struct {
	Channel     string    `json:"channel"`
	Tag         string    `json:"tag"`
	Image       string    `json:"image"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	HTMLURL     string    `json:"html_url,omitempty"`
}

func New(
	cfg Config,
	credsRepo repository.CredentialsRepository,
	workerRepo repository.WorkerRepository,
	orchestrator *worker_orchestrator.Orchestrator,
) *Service {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Service{
		cfg:          cfg,
		credsRepo:    credsRepo,
		workerRepo:   workerRepo,
		orchestrator: orchestrator,
		http:         cfg.HTTPClient,
		state: State{
			Channels:   map[string]ChannelView{},
			GithubRepo: cfg.GithubRepo,
			ImageRepo:  cfg.WorkerImageRepo,
			Enabled:    cfg.Enabled,
		},
	}
}

// public

// CheckGitHub resolves stable + dev channels by hitting the GitHub Releases
// API, updates each profile that subscribes to a channel, and (if the
// profile has auto_update=true) rolls every assigned worker to the new
// image.
//
// Returns the list of profiles that changed and the resolved state.
func (s *Service) CheckGitHub(ctx context.Context) (changed []ProfileUpdate, err error) {
	if !s.cfg.Enabled {
		return nil, errors.New("releases not enabled")
	}

	releases, err := s.fetchReleases(ctx)
	if err != nil {
		s.recordError(err.Error())
		return nil, err
	}

	stable, dev := pickChannelHeads(releases)

	now := time.Now()
	s.stateMu.Lock()
	s.state.LastCheckedAt = now
	s.state.LastError = ""
	s.state.Channels = map[string]ChannelView{}
	if stable != nil {
		s.state.Channels["stable"] = s.channelView("stable", stable)
	}
	if dev != nil {
		s.state.Channels["dev"] = s.channelView("dev", dev)
	}
	s.stateMu.Unlock()

	for _, channel := range []models.ReleaseChannel{models.ReleaseChannelStable, models.ReleaseChannelDev} {
		var target *githubRelease
		switch channel {
		case models.ReleaseChannelStable:
			target = stable
		case models.ReleaseChannelDev:
			target = dev
		}
		if target == nil {
			continue
		}
		image := s.imageFor(target.TagName)

		profiles, err := s.credsRepo.ListProfilesByChannel(ctx, channel)
		if err != nil {
			return changed, fmt.Errorf("list profiles for %s: %w", channel, err)
		}
		for _, p := range profiles {
			if p.WorkerImage == image && p.ResolvedImageTag == target.TagName {
				continue // already on the latest
			}
			if err := s.credsRepo.RecordResolvedTag(ctx, p.ID, image, target.TagName); err != nil {
				log.Printf("releases: record tag for %s: %v", p.Name, err)
				continue
			}
			pu := ProfileUpdate{
				ProfileID:   p.ID,
				ProfileName: p.Name,
				Channel:     string(channel),
				NewTag:      target.TagName,
				NewImage:    image,
				AutoApplied: p.AutoUpdate,
			}
			if p.AutoUpdate {
				pu.Rollout = s.rollout(ctx, p.ID, image)
			}
			changed = append(changed, pu)
		}
	}

	return changed, nil
}

type ProfileUpdate struct {
	ProfileID   uuid.UUID      `json:"profile_id"`
	ProfileName string         `json:"profile_name"`
	Channel     string         `json:"channel"`
	NewTag      string         `json:"new_tag"`
	NewImage    string         `json:"new_image"`
	AutoApplied bool           `json:"auto_applied"`
	Rollout     []RolloutEntry `json:"rollout,omitempty"`
}

type RolloutEntry struct {
	WorkerID uuid.UUID `json:"worker_id"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Skipped  string    `json:"skipped,omitempty"`
}

// HandleWebhook validates the GitHub `release` event signature and triggers
// a CheckGitHub. Returns 401 on signature mismatch — only the secret holder
// can fire updates.
func (s *Service) HandleWebhook(ctx context.Context, body []byte, signature, eventType string) error {
	if !s.cfg.Enabled {
		return errors.New("releases not enabled")
	}
	if s.cfg.WebhookSecret == "" {
		return errors.New("webhook secret not configured")
	}
	if !verifySignature(s.cfg.WebhookSecret, body, signature) {
		return errors.New("bad signature")
	}
	// Only react to release publish events. GitHub fires several action types
	// for `release`; published/released are the ones that mean "new artifact
	// is live."
	if eventType == "release" {
		var ev struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(body, &ev)
		switch ev.Action {
		case "published", "released", "edited", "":
			_, err := s.CheckGitHub(ctx)
			return err
		default:
			return nil // ignore (created/prereleased/deleted etc.)
		}
	}
	// For ping or other events, just succeed silently.
	return nil
}

// GetState returns the last known per-channel resolution. Cheap; reads from
// the in-memory cache populated by CheckGitHub.
func (s *Service) GetState() State {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	st := s.state
	chans := make(map[string]ChannelView, len(s.state.Channels))
	for k, v := range s.state.Channels {
		chans[k] = v
	}
	st.Channels = chans
	return st
}

// RunBootCheck is fire-and-forget: when the backend starts, sync state once
// so the dashboard isn't empty. Errors are logged, not surfaced.
func (s *Service) RunBootCheck(ctx context.Context) {
	if !s.cfg.Enabled {
		log.Printf("releases: disabled")
		return
	}
	if s.cfg.GithubRepo == "" || s.cfg.WorkerImageRepo == "" {
		log.Printf("releases: skipping boot check (RELEASES_GITHUB_REPO or RELEASES_WORKER_IMAGE_REPO unset)")
		return
	}
	go func() {
		bctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.CheckGitHub(bctx); err != nil {
			log.Printf("releases: boot check failed: %v", err)
		}
	}()
}

// internals

func (s *Service) rollout(ctx context.Context, profileID uuid.UUID, image string) []RolloutEntry {
	workers, err := s.workerRepo.ListWorkersByProfile(ctx, profileID)
	if err != nil {
		return []RolloutEntry{{OK: false, Error: "list workers: " + err.Error()}}
	}
	out := make([]RolloutEntry, 0, len(workers))
	for _, w := range workers {
		if w.InstallState != models.WorkerInstallStateInstalled {
			out = append(out, RolloutEntry{WorkerID: w.ID, OK: false, Skipped: "not installed"})
			continue
		}
		if err := s.orchestrator.UpdateToImage(ctx, w.ID, image); err != nil {
			out = append(out, RolloutEntry{WorkerID: w.ID, OK: false, Error: err.Error()})
			continue
		}
		out = append(out, RolloutEntry{WorkerID: w.ID, OK: true})
	}
	return out
}

func (s *Service) recordError(msg string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.state.LastError = msg
	s.state.LastCheckedAt = time.Now()
}

func (s *Service) imageFor(tag string) string {
	repo := strings.TrimRight(s.cfg.WorkerImageRepo, "/")
	return repo + ":" + tag
}

func (s *Service) channelView(name string, r *githubRelease) ChannelView {
	return ChannelView{
		Channel:     name,
		Tag:         r.TagName,
		Image:       s.imageFor(r.TagName),
		PublishedAt: r.PublishedAt,
		HTMLURL:     r.HTMLURL,
	}
}

// GitHub API

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

func (s *Service) fetchReleases(ctx context.Context) ([]githubRelease, error) {
	if s.cfg.GithubRepo == "" {
		return nil, errors.New("RELEASES_GITHUB_REPO not set")
	}
	url := "https://api.github.com/repos/" + s.cfg.GithubRepo + "/releases?per_page=30"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if s.cfg.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.GithubToken)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return releases, nil
}

// pickChannelHeads returns the most recent published release for each channel:
//   - stable: latest non-prerelease, non-draft
//   - dev:    latest published (including prereleases)
func pickChannelHeads(releases []githubRelease) (stable, dev *githubRelease) {
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].PublishedAt.After(releases[j].PublishedAt)
	})
	for i := range releases {
		r := &releases[i]
		if r.Draft {
			continue
		}
		if dev == nil {
			dev = r
		}
		if !r.Prerelease && stable == nil {
			stable = r
		}
		if dev != nil && stable != nil {
			break
		}
	}
	return
}

// HMAC verification

func verifySignature(secret string, body []byte, header string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
