package mailvendor

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// workspace is one vendor workspace (or organization) the key reaches.
type workspace struct {
	ID   string
	Name string
}

// workspaceTTL bounds how long a discovered workspace list is reused, so one added at the vendor shows up.
const workspaceTTL = 5 * time.Minute

// workspaceCache holds the workspaces a key reaches; vendors that scope every call to one are read across all of them.
type workspaceCache struct {
	mu   sync.Mutex
	list []workspace
	at   time.Time
}

func (w *workspaceCache) get(ctx context.Context, fetch func(context.Context) ([]workspace, error)) ([]workspace, error) {
	w.mu.Lock()
	if w.at.IsZero() || time.Since(w.at) >= workspaceTTL {
		w.mu.Unlock()
		list, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		w.mu.Lock()
		w.list, w.at = list, time.Now()
	}
	out := append([]workspace(nil), w.list...)
	w.mu.Unlock()
	return out, nil
}

// scopeSep joins a workspace id to an id the vendor only resolves inside that workspace.
const scopeSep = ":"

func scoped(ws, id string) string {
	if ws == "" || id == "" {
		return id
	}
	return ws + scopeSep + id
}

// unscoped splits a scoped id; an id stored before scoping comes back with no workspace.
func unscoped(id string) (ws, rest string) {
	if i := strings.Index(id, scopeSep); i > 0 {
		return id[:i], id[i+len(scopeSep):]
	}
	return "", id
}

// findCredentials asks each workspace in turn, for an id stored before ids carried their workspace.
func findCredentials(vendor string, wss []workspace, fn func(workspace) (Credentials, error)) (Credentials, error) {
	refused, read := error(nil), 0
	for _, ws := range wss {
		cr, err := fn(ws)
		switch {
		case err == nil:
			return cr, nil
		case errors.Is(err, ErrUnauthorized):
			refused = err
		case errors.Is(err, ErrRateLimited), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return Credentials{}, err
		default:
			read++
		}
	}
	if read == 0 && refused != nil {
		return Credentials{}, refused
	}
	return Credentials{}, vendorErr(vendor, http.StatusOK, "not found", ErrNotFound)
}

// errStop ends eachWorkspace early without an error, once a cap is reached.
var errStop = errors.New("mailvendor: stop")

// eachWorkspace runs fn in every workspace. One the key may not read is skipped, unless the key can read none.
func eachWorkspace(wss []workspace, fn func(workspace) error) error {
	var refused error
	read := 0
	for _, ws := range wss {
		err := fn(ws)
		switch {
		case err == nil:
			read++
		case errors.Is(err, errStop):
			return nil
		case errors.Is(err, ErrUnauthorized):
			refused = err
		default:
			return err
		}
	}
	if read == 0 && refused != nil {
		return refused
	}
	return nil
}
