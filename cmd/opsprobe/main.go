// Command opsprobe is the external platform health probe. It names TLS, DNS,
// API, event ingest, read-after-write, and worker heartbeat. Partial failure
// is not green. GET /health HTTP 200 is never enough.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/warmbly/warmbly/internal/app/plathealth"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("opsprobe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("base-url", "", "API base URL (for example http://127.0.0.1:8080)")
	natsURL := fs.String("nats-url", "", "optional NATS URL for a live bus round-trip")
	fixture := fs.String("fixture", "", "JSON ProbeInput fixture (offline path)")
	timeout := fs.Duration("timeout", plathealth.DefaultTimeout, "per-check timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *fixture == "" && *base == "" && *natsURL == "" {
		fmt.Fprintf(stderr, "usage: opsprobe --fixture PATH | --base-url URL [--nats-url URL]\n")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*(*timeout)+time.Second)
	defer cancel()
	report, err := plathealth.RunProbe(ctx, plathealth.ProbeConfig{
		BaseURL:     *base,
		NATSURL:     *natsURL,
		FixturePath: *fixture,
		Timeout:     *timeout,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	raw, err := plathealth.MarshalProbe(report)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if hits := plathealth.PIIFindings(raw); len(hits) > 0 {
		fmt.Fprintf(stderr, "refusing to print payload with pii: %v\n", hits)
		return 1
	}
	if _, err := stdout.Write(append(raw, '\n')); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if report.Green {
		return 0
	}
	return 1
}
