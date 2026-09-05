// Package iostreams is the CLI's terminal: where output goes, whether anyone
// is watching, and how to ask a question.
//
// The rule the whole CLI depends on: nothing prompts when stdin is not a
// terminal. A command that would need an answer fails with the flag that
// supplies it instead, which is what makes the surface scriptable.
package iostreams

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinTTY  bool
	stdoutTTY bool
	color     bool
	width     int
}

// System builds the streams from the real process, reading the environment
// conventions everyone already expects: NO_COLOR off, FORCE_COLOR on.
func System() *IOStreams {
	s := &IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	s.stdinTTY = term.IsTerminal(int(os.Stdin.Fd()))
	s.stdoutTTY = term.IsTerminal(int(os.Stdout.Fd()))
	s.width = 80
	if s.stdoutTTY {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
			s.width = w
		}
	}
	s.color = s.stdoutTTY && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	if os.Getenv("FORCE_COLOR") != "" {
		s.color = true
	}
	return s
}

func (s *IOStreams) IsStdinTTY() bool  { return s.stdinTTY }
func (s *IOStreams) IsStdoutTTY() bool { return s.stdoutTTY }
func (s *IOStreams) ColorEnabled() bool {
	return s.color
}
func (s *IOStreams) TerminalWidth() int { return s.width }

// SetColor forces colour on or off (--no-color, or a test).
func (s *IOStreams) SetColor(on bool) { s.color = on }

func (s *IOStreams) Printf(format string, a ...any) { fmt.Fprintf(s.Out, format, a...) }
func (s *IOStreams) Println(a ...any)               { fmt.Fprintln(s.Out, a...) }
func (s *IOStreams) Errorf(format string, a ...any) { fmt.Fprintf(s.ErrOut, format, a...) }
func (s *IOStreams) Errorln(a ...any)               { fmt.Fprintln(s.ErrOut, a...) }

// Colour helpers. Each one is a no-op when colour is off, so call sites never
// branch and piped output never carries escape codes.
func (s *IOStreams) paint(code, text string) string {
	if !s.color {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func (s *IOStreams) Bold(t string) string    { return s.paint("1", t) }
func (s *IOStreams) Dim(t string) string     { return s.paint("2", t) }
func (s *IOStreams) Red(t string) string     { return s.paint("31", t) }
func (s *IOStreams) Green(t string) string   { return s.paint("32", t) }
func (s *IOStreams) Yellow(t string) string  { return s.paint("33", t) }
func (s *IOStreams) Blue(t string) string    { return s.paint("34", t) }
func (s *IOStreams) Magenta(t string) string { return s.paint("35", t) }
func (s *IOStreams) Cyan(t string) string    { return s.paint("36", t) }
func (s *IOStreams) Gray(t string) string    { return s.paint("90", t) }

// Icons that degrade to ASCII, because a Windows console or a CI log should
// not render boxes.
func (s *IOStreams) Tick() string {
	if s.color {
		return s.Green("✓")
	}
	return "ok"
}

func (s *IOStreams) Cross() string {
	if s.color {
		return s.Red("✗")
	}
	return "x"
}

// ErrNoTTY is what every prompt returns when nobody is there to answer.
type ErrNoTTY struct{ Need string }

func (e *ErrNoTTY) Error() string {
	return "this needs an answer and there is no terminal to ask on. " + e.Need
}

// Confirm asks a yes/no question. def is the answer a bare Enter gives.
func (s *IOStreams) Confirm(question string, def bool) (bool, error) {
	if !s.stdinTTY {
		return false, &ErrNoTTY{Need: "Pass --yes to answer yes without being asked."}
	}
	suffix := " [y/N] "
	if def {
		suffix = " [Y/n] "
	}
	reader := bufio.NewReader(s.In)
	for {
		fmt.Fprint(s.ErrOut, question+suffix)
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(s.ErrOut, "Please answer y or n.")
	}
}

// Input asks for a line of text, offering def when the answer is empty.
func (s *IOStreams) Input(question, def string) (string, error) {
	if !s.stdinTTY {
		return "", &ErrNoTTY{Need: "Supply it with a flag instead."}
	}
	prompt := question
	if def != "" {
		prompt += " (" + def + ")"
	}
	fmt.Fprint(s.ErrOut, prompt+": ")
	line, err := bufio.NewReader(s.In).ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// Secret reads a value without echoing it. Used for pasting an API key.
func (s *IOStreams) Secret(question string) (string, error) {
	if !s.stdinTTY {
		// A piped secret is the documented CI path, so read it plainly.
		// ReadString returns io.EOF alongside the final line when the input
		// ends without a newline, which `printf '%s' "$KEY" |` always does;
		// treating that as a failure would reject the exact form CI uses.
		line, err := bufio.NewReader(s.In).ReadString('\n')
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return strings.TrimSpace(line), err
	}
	fmt.Fprint(s.ErrOut, question+": ")
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(s.ErrOut)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// Select asks the user to pick one of a list. On a terminal it draws an
// arrow-key menu; anywhere else it is a numbered prompt, which is also the
// fallback when raw mode is unavailable.
func (s *IOStreams) Select(question string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, fmt.Errorf("nothing to choose from")
	}
	if len(options) == 1 {
		return 0, nil
	}
	if !s.stdinTTY {
		return 0, &ErrNoTTY{Need: "Supply the choice with a flag instead."}
	}
	if idx, err := s.selectInteractive(question, options); err == nil {
		return idx, nil
	}
	return s.selectNumbered(question, options)
}

func (s *IOStreams) selectNumbered(question string, options []string) (int, error) {
	fmt.Fprintln(s.ErrOut, question)
	for i, o := range options {
		fmt.Fprintf(s.ErrOut, "  %d) %s\n", i+1, o)
	}
	reader := bufio.NewReader(s.In)
	for {
		fmt.Fprintf(s.ErrOut, "Choose 1-%d [1]: ", len(options))
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(line)
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		fmt.Fprintln(s.ErrOut, "Not one of the options.")
	}
}

// selectInteractive is the arrow-key menu. Every redraw rewinds exactly as
// many lines as it printed, so a wrapped line would draw over the screen
// above it: each option is clipped to the terminal width first.
func (s *IOStreams) selectInteractive(question string, options []string) (int, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, err
	}
	defer func() { _ = term.Restore(fd, state) }()

	cursor := 0
	draw := func(first bool) {
		if !first {
			fmt.Fprintf(s.ErrOut, "\033[%dA", len(options))
		}
		for i, o := range options {
			prefix := "  "
			line := o
			if i == cursor {
				prefix = s.Cyan("> ")
				line = s.Bold(o)
			}
			fmt.Fprintf(s.ErrOut, "\r\033[K%s%s\r\n", prefix, s.clip(line, 2))
		}
	}

	fmt.Fprintf(s.ErrOut, "%s %s\r\n", question, s.Dim("(arrows or j/k, enter to choose)"))
	draw(true)

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0, err
		}
		switch {
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			return cursor, nil
		case n == 1 && (buf[0] == 3 || buf[0] == 27): // ctrl-c, esc
			return 0, fmt.Errorf("cancelled")
		case n == 1 && (buf[0] == 'j' || buf[0] == 'J'):
			cursor = (cursor + 1) % len(options)
		case n == 1 && (buf[0] == 'k' || buf[0] == 'K'):
			cursor = (cursor - 1 + len(options)) % len(options)
		case n >= 3 && buf[0] == 27 && buf[1] == '[':
			switch buf[2] {
			case 'B':
				cursor = (cursor + 1) % len(options)
			case 'A':
				cursor = (cursor - 1 + len(options)) % len(options)
			}
		default:
			// Digits pick directly, which is faster than arrowing down a list.
			if n == 1 && buf[0] >= '1' && buf[0] <= '9' {
				if idx := int(buf[0] - '1'); idx < len(options) {
					cursor = idx
					return cursor, nil
				}
			}
			continue
		}
		draw(false)
	}
}

// clip truncates to the terminal width, counting a prefix the caller already
// printed. Colour codes are invisible, so they are measured out first.
func (s *IOStreams) clip(text string, used int) string {
	limit := s.width - used - 1
	if limit < 10 {
		limit = 10
	}
	if visibleLen(text) <= limit {
		return text
	}
	// Truncating a coloured string mid-escape would leak codes; strip first.
	plain := StripANSI(text)
	if len(plain) <= limit {
		return plain
	}
	return plain[:limit-1] + "…"
}

func visibleLen(s string) int { return len([]rune(StripANSI(s))) }

// StripANSI removes SGR escape sequences, so widths can be measured and
// redirected output never carries colour.
func StripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
