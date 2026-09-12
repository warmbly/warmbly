package eventbus

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/nats-io/nkeys"
)

// Built at run time from a real throwaway keypair: an nkey seed carries a
// checksum, so a hand-written one cannot parse, and embedding a valid seed in
// the repo would put key-shaped material in git for no reason.
func sampleCredsFile(t *testing.T) string {
	t.Helper()
	kp, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatal(err)
	}
	return "-----BEGIN NATS USER JWT-----\n" +
		"eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5LW5rZXkifQ.eyJzdWIiOiJVQUEifQ.c2ln\n" +
		"------END NATS USER JWT------\n\n" +
		"-----BEGIN USER NKEY SEED-----\n" +
		string(seed) + "\n" +
		"------END USER NKEY SEED------\n"
}

// The fleet ships env files through docker --env-file, which cannot express a
// multi-line value, so the base64 form is the one that has to work.
func TestCredsOptionFromBase64(t *testing.T) {
	t.Setenv("NATS_CREDS_B64", base64.StdEncoding.EncodeToString([]byte(sampleCredsFile(t))))
	t.Setenv("NATS_CREDS", "")
	opt, err := credsOption()
	if err != nil {
		t.Fatalf("credsOption: %v", err)
	}
	if opt == nil {
		t.Fatal("NATS_CREDS_B64 was set but produced no option")
	}
}

func TestCredsOptionFromFile(t *testing.T) {
	f := t.TempDir() + "/u.creds"
	if err := os.WriteFile(f, []byte(sampleCredsFile(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NATS_CREDS_B64", "")
	t.Setenv("NATS_CREDS", f)
	opt, err := credsOption()
	if err != nil || opt == nil {
		t.Fatalf("file form: opt=%v err=%v", opt, err)
	}
}

// Neither set must stay silent: token and user-password URLs still work, and
// erroring here would break every existing deployment.
func TestCredsOptionAbsent(t *testing.T) {
	t.Setenv("NATS_CREDS_B64", "")
	t.Setenv("NATS_CREDS", "")
	opt, err := credsOption()
	if err != nil || opt != nil {
		t.Fatalf("expected no option and no error, got opt=%v err=%v", opt, err)
	}
}

// A truncated or mis-pasted value must say so at boot rather than fail as an
// unexplained authorization error against the bus.
func TestCredsOptionRejectsGarbage(t *testing.T) {
	t.Setenv("NATS_CREDS", "")
	t.Setenv("NATS_CREDS_B64", "not-base64!!")
	if _, err := credsOption(); err == nil {
		t.Error("invalid base64 was accepted")
	}
	t.Setenv("NATS_CREDS_B64", base64.StdEncoding.EncodeToString([]byte("no jwt here")))
	if _, err := credsOption(); err == nil {
		t.Error("credentials with no JWT were accepted")
	}
}
