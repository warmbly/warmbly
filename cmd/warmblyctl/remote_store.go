package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const credentialLimit = 1 << 20

type remoteToken struct {
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

func (t remoteToken) valid(now time.Time) bool {
	return t.AccessToken != "" && t.RefreshToken != "" && !strings.ContainsAny(t.AccessToken+t.RefreshToken, "\r\n") && t.AccessTokenExpiresAt.After(now) && t.RefreshTokenExpiresAt.After(now)
}

type remoteCredential struct {
	URL    string      `json:"url"`
	UserID string      `json:"user_id"`
	Token  remoteToken `json:"token"`
}

type remoteState struct {
	Version     int                         `json:"version"`
	Servers     map[string]string           `json:"servers"`
	Credentials map[string]remoteCredential `json:"credentials"`
}

type remoteStore struct {
	root  *os.Root
	lock  *os.File
	state remoteState
}

func remoteCredentialKey(base, profile string) string {
	sum := sha256.Sum256([]byte(base + "\x00" + profile))
	return hex.EncodeToString(sum[:])
}

func remoteStorePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" || !filepath.IsAbs(base) {
		return "", remoteFailure(5, "unsafe_storage", "A private absolute user configuration directory is required.")
	}
	return filepath.Join(base, "warmblyctl"), nil
}

func openRemoteStore(ctx context.Context, path string) (*remoteStore, error) {
	if !filepath.IsAbs(path) {
		return nil, storageFailure()
	}
	path, err := canonicalRemoteStorePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, storageFailure()
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, storageFailure()
	}
	s := &remoteStore{root: root, state: remoteState{Version: 1, Servers: map[string]string{}, Credentials: map[string]remoteCredential{}}}
	info, err := root.Lstat(".")
	if err != nil || !privateRemoteFile(info, true) {
		s.close()
		return nil, storageFailure()
	}
	s.lock, err = s.openPrivate("lock", true)
	if err != nil {
		s.close()
		return nil, err
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		locked, err := lockRemoteFile(s.lock)
		if err != nil {
			s.close()
			return nil, storageFailure()
		}
		if locked {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.close()
			return nil, remoteFailure(7, "cancelled", "Operation cancelled.")
		case <-deadline.C:
			timer.Stop()
			s.close()
			return nil, remoteFailure(5, "profile_busy", "Another warmblyctl process is using the credential store. Retry later.")
		case <-timer.C:
		}
	}
	f, err := s.openPrivate("credentials.json", false)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		s.close()
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, credentialLimit+1))
	_, decodeErr := decodeRemoteJSON(b)
	if err != nil || len(b) > credentialLimit || decodeErr != nil || json.Unmarshal(b, &s.state) != nil || s.state.Version != 1 || s.state.Servers == nil || s.state.Credentials == nil {
		s.close()
		return nil, storageFailure()
	}
	return s, nil
}

// Resolve existing ancestors once; all subsequent operations use the canonical, pinned root.
func canonicalRemoteStorePath(path string) (string, error) {
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil {
		if !privateRemoteFile(info, true) {
			return "", storageFailure()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", storageFailure()
	}
	p, suffix := filepath.Dir(path), []string{filepath.Base(path)}
	for {
		_, err := os.Lstat(p)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(p) == p {
			return "", storageFailure()
		}
		suffix = append(suffix, filepath.Base(p))
		p = filepath.Dir(p)
	}
	canonical, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", storageFailure()
	}
	for ancestor := canonical; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Lstat(ancestor)
		if err != nil || !trustedRemoteAncestor(info) {
			return "", storageFailure()
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		canonical = filepath.Join(canonical, suffix[i])
	}
	return canonical, nil
}

func storageFailure() error {
	return remoteFailure(5, "unsafe_storage", "Credential storage requires trusted ancestors, an owner-only pinned directory, and regular owner-only files with no symlinks or hard links on a supported POSIX filesystem.")
}

func (s *remoteStore) openPrivate(name string, create bool) (*os.File, error) {
	info, err := s.root.Lstat(name)
	if err == nil && !privateRemoteFile(info, false) {
		return nil, storageFailure()
	}
	flags := os.O_RDWR
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, os.ErrNotExist
		}
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil {
		return nil, storageFailure()
	}
	f, err := s.root.OpenFile(name, flags|remoteNoFollow(), 0600)
	if err != nil {
		return nil, storageFailure()
	}
	opened, err := f.Stat()
	if err != nil || !privateRemoteFile(opened, false) || (info != nil && !os.SameFile(info, opened)) {
		f.Close()
		return nil, storageFailure()
	}
	return f, nil
}

func (s *remoteStore) save() error {
	if info, err := s.root.Lstat("credentials.json"); err == nil {
		if !privateRemoteFile(info, false) {
			return storageFailure()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return storageFailure()
	}
	b, err := json.Marshal(s.state)
	if err != nil || len(b) > credentialLimit {
		return storageFailure()
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return storageFailure()
	}
	name := ".credentials-" + hex.EncodeToString(nonce[:])
	f, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|remoteNoFollow(), 0600)
	if err != nil {
		return storageFailure()
	}
	defer s.root.Remove(name) // Also cleans up a failed atomic replacement.
	_, werr := f.Write(b)
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil || serr != nil || cerr != nil || s.root.Rename(name, "credentials.json") != nil {
		return storageFailure()
	}
	dir, err := s.root.Open(".")
	if err != nil {
		return storageFailure()
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return storageFailure()
	}
	return nil
}

func (s *remoteStore) close() {
	if s.lock != nil {
		s.lock.Close()
	}
	if s.root != nil {
		s.root.Close()
	}
}
