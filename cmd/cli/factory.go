package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/config"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
	"github.com/warmbly/warmbly/internal/cli/output"
	"github.com/warmbly/warmbly/internal/version"
)

// Factory is what every command is handed: the terminal, the two config files,
// and a way to build an authenticated client. Building it lazily matters,
// because `warmbly auth login` and `warmbly version` must work before there is
// anything to authenticate with.
type Factory struct {
	IO *iostreams.IOStreams

	// Global flags, bound once on the root command.
	HostFlag  string
	JSONOut   bool
	Template  string
	Fields    []string
	AssumeYes bool
	NoColor   bool
	Debug     bool

	cfg   *config.Config
	hosts config.Hosts
}

func NewFactory() *Factory {
	return &Factory{IO: iostreams.System()}
}

// UserAgent identifies the CLI in API usage logs, which is how an operator
// tells a CLI call from a script's.
func UserAgent() string {
	return fmt.Sprintf("warmbly-cli/%s (%s/%s)", version.String(), runtime.GOOS, runtime.GOARCH)
}

func (f *Factory) Config() (*config.Config, error) {
	if f.cfg != nil {
		return f.cfg, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	f.cfg = cfg
	return cfg, nil
}

func (f *Factory) Hosts() (config.Hosts, error) {
	if f.hosts != nil {
		return f.hosts, nil
	}
	hosts, err := config.LoadHosts()
	if err != nil {
		return nil, err
	}
	f.hosts = hosts
	return hosts, nil
}

// Resolved answers which host and token this invocation uses.
func (f *Factory) Resolved() (*config.Resolved, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}
	hosts, err := f.Hosts()
	if err != nil {
		return nil, err
	}
	return config.Resolve(cfg, hosts, f.HostFlag)
}

// Client builds an authenticated client, or explains how to get one.
func (f *Factory) Client() (*api.Client, error) {
	r, err := f.Resolved()
	if err != nil {
		return nil, err
	}
	c := api.New(r.APIURL, r.Token, UserAgent())
	if f.Debug {
		c.Debug = f.IO.ErrOut
	}
	return c, nil
}

// Printer is the renderer for this invocation, honouring the config default
// and then the flags.
func (f *Factory) Printer() *output.Printer {
	jsonOut := f.JSONOut
	if !jsonOut {
		if cfg, err := f.Config(); err == nil && cfg.Get("output") == "json" {
			jsonOut = true
		}
	}
	return &output.Printer{IO: f.IO, JSON: jsonOut, Template: f.Template, Fields: f.Fields}
}

// ConfirmSend is the gate in front of anything that puts real mail on the
// wire. Without a terminal it refuses rather than sending, because a script
// that forgot --yes must not discover the omission by mailing strangers.
func (f *Factory) ConfirmSend(what string) error {
	if f.AssumeYes {
		return nil
	}
	if !f.IO.IsStdinTTY() {
		return fmt.Errorf("%s sends real mail, and there is no terminal to confirm on.\nPass --yes to go ahead.", what)
	}
	ok, err := f.IO.Confirm(f.IO.Yellow("! ")+what+" sends real mail. Continue?", false)
	if err != nil {
		return err
	}
	if !ok {
		return errCancelled
	}
	return nil
}

// ConfirmMutation guards a destructive but non-sending change. It only asks
// when the user opted in with `warmbly config set confirm always`, because
// prompting on every write makes a CLI tiring to use.
func (f *Factory) ConfirmMutation(what string) error {
	if f.AssumeYes {
		return nil
	}
	cfg, err := f.Config()
	if err != nil || cfg.Get("confirm") != "always" {
		return nil
	}
	if !f.IO.IsStdinTTY() {
		return nil
	}
	ok, cerr := f.IO.Confirm(what+"?", false)
	if cerr != nil {
		return cerr
	}
	if !ok {
		return errCancelled
	}
	return nil
}

// bodyFromArg turns a --input value into a request body: a JSON literal, `-`
// for stdin, or @path for a file, the conventions curl taught everyone.
func bodyFromArg(in io.Reader, data string) ([]byte, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}
	switch {
	case data == "-":
		return io.ReadAll(in)
	case strings.HasPrefix(data, "@"):
		return os.ReadFile(strings.TrimPrefix(data, "@"))
	default:
		return []byte(data), nil
	}
}
