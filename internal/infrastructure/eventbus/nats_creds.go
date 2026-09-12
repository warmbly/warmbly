package eventbus

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
)

// credsOption resolves the NATS user credential from the environment.
//
// A managed bus (Synadia Cloud and anything else using JWT auth) authenticates
// with a .creds file holding a user JWT and an nkey seed, which no URL can
// carry. Two forms, because the two places this runs cannot use the same one:
//
//	NATS_CREDS      path to the file. Containers and local development.
//	NATS_CREDS_B64  the file, base64 encoded. A worker joins with an env file
//	                that docker passes via --env-file, which cannot express a
//	                multi-line value, so the fleet needs a single-line form.
//
// Base64 rather than separate JWT and seed variables: it is one value, it is
// exactly what Synadia hands you, and there is no chance of pasting the two
// halves the wrong way round. It is the shape NATS's own NEX project uses.
//
// Returns nil when neither is set, so token and user-password URLs keep working.
func credsOption() (nats.Option, error) {
	if raw := strings.TrimSpace(os.Getenv("NATS_CREDS_B64")); raw != "" {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("eventbus nats: NATS_CREDS_B64 is not valid base64: %w", err)
		}
		return credsFromContents(decoded)
	}
	if path := strings.TrimSpace(os.Getenv("NATS_CREDS")); path != "" {
		return nats.UserCredentials(path), nil
	}
	return nil, nil
}

// credsFromContents builds the option without writing the secret to disk.
// nats.UserCredentials only takes a path, and a temp file would leave the
// seed readable to anything else on the host for the life of the process.
func credsFromContents(contents []byte) (nats.Option, error) {
	userJWT, err := jwt.ParseDecoratedJWT(contents)
	if err != nil {
		return nil, fmt.Errorf("eventbus nats: no user JWT in the credentials: %w", err)
	}
	kp, err := jwt.ParseDecoratedUserNKey(contents)
	if err != nil {
		return nil, fmt.Errorf("eventbus nats: no nkey seed in the credentials: %w", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		return nil, fmt.Errorf("eventbus nats: credentials carry no private seed: %w", err)
	}
	return nats.UserJWTAndSeed(userJWT, string(seed)), nil
}
