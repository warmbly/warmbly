package worker_orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// dialer wraps an *ssh.Client with helpers tailored for what the orchestrator
// actually needs: run commands, upload small files, with TOFU host-key pinning.
type dialer struct {
	client *ssh.Client
	// fingerprint observed during this connect. Caller stores it back into the
	// workers row on first connect for future verification.
	observedFingerprint string
}

type dialOptions struct {
	host                string
	port                int
	user                string
	signer              ssh.Signer
	password            string // optional; used only when signer is nil
	expectedFingerprint string // empty on first connect (TOFU)
	timeout             time.Duration
}

func dial(ctx context.Context, opts dialOptions) (*dialer, error) {
	if opts.port == 0 {
		opts.port = 22
	}
	if opts.user == "" {
		opts.user = "root"
	}
	if opts.timeout == 0 {
		opts.timeout = 15 * time.Second
	}

	var auths []ssh.AuthMethod
	if opts.signer != nil {
		auths = append(auths, ssh.PublicKeys(opts.signer))
	}
	if opts.password != "" {
		auths = append(auths, ssh.Password(opts.password))
	}
	if len(auths) == 0 {
		return nil, errors.New("no SSH auth method configured")
	}

	d := &dialer{}
	hostKeyCallback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fp := FingerprintSHA256(key)
		d.observedFingerprint = fp
		if opts.expectedFingerprint == "" {
			return nil // TOFU: accept and pin
		}
		if fp != opts.expectedFingerprint {
			return fmt.Errorf("host key mismatch: pinned %s, got %s", opts.expectedFingerprint, fp)
		}
		return nil
	}

	cfg := &ssh.ClientConfig{
		User:            opts.user,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback,
		Timeout:         opts.timeout,
	}

	addr := net.JoinHostPort(opts.host, strconv.Itoa(opts.port))

	// Honour context cancellation while net dial would otherwise block.
	type dialResult struct {
		conn net.Conn
		err  error
	}
	resCh := make(chan dialResult, 1)
	go func() {
		c, err := (&net.Dialer{Timeout: opts.timeout}).DialContext(ctx, "tcp", addr)
		resCh <- dialResult{c, err}
	}()
	var rawConn net.Conn
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-resCh:
		if r.err != nil {
			return nil, fmt.Errorf("dial %s: %w", addr, r.err)
		}
		rawConn = r.conn
	}

	c, chans, reqs, err := ssh.NewClientConn(rawConn, addr, cfg)
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	d.client = ssh.NewClient(c, chans, reqs)
	return d, nil
}

func (d *dialer) Close() error {
	if d.client == nil {
		return nil
	}
	return d.client.Close()
}

// Run executes a command, returning combined stdout/stderr. Stderr is appended
// to stdout because most of what we run is `set -e` shell that interleaves.
func (d *dialer) Run(ctx context.Context, cmd string) (string, error) {
	sess, err := d.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	var out bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()

	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGINT)
		return out.String(), ctx.Err()
	case err := <-done:
		return out.String(), err
	}
}

// Upload writes content to a remote path via stdin to `tee`. Avoids requiring
// an scp/sftp subsystem on the target.
func (d *dialer) Upload(ctx context.Context, remotePath, content string, mode string) error {
	sess, err := d.client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("install -D -m %s /dev/stdin %s", mode, shellQuote(remotePath))
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start tee: %w", err)
	}
	if _, err := io.WriteString(stdin, content); err != nil {
		return err
	}
	if err := stdin.Close(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGINT)
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
