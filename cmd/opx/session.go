package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// sessionVar is the environment variable op reads a session token from.
type sessionVar struct {
	Name, Token string
}

func (v sessionVar) env() string {
	return v.Name + "=" + v.Token
}

func (v sessionVar) export() string {
	return fmt.Sprintf("export %s=\"%s\"\n", v.Name, v.Token)
}

// exportRe matches the export line of op signin and of the session file.
// It is strict: the name and token end up in eval in a shell.
var exportRe = regexp.MustCompile(`(?m)^export (OP_SESSION_[A-Za-z0-9_]+)="([A-Za-z0-9_-]+)"$`)

type opCall struct {
	args        []string
	session     sessionVar // set in the environment if not zero
	stdin       io.Reader
	interactive bool // op may prompt on the terminal
}

type sessions struct {
	profile  Profile
	dir      string
	op       func(context.Context, opCall) ([]byte, error)
	terminal bool                                  // stdin is a terminal op can ask the password on
	ask      func(context.Context) (string, error) // asks for the password without a terminal
}

func newSessions(profile Profile) (*sessions, error) {
	dir, err := sessionDir()
	if err != nil {
		return nil, err
	}
	return &sessions{
		profile:  profile,
		dir:      dir,
		op:       runOp,
		terminal: term.IsTerminal(int(os.Stdin.Fd())),
	}, nil
}

// sessionDir is cleared at logout or reboot: the runtime directory, or on
// macOS, which has none, the per-user $TMPDIR.
func sessionDir() (string, error) {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "opx"), nil
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(os.TempDir(), "opx"), nil
	}
	return "", errors.New("XDG_RUNTIME_DIR is not set")
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// file is the path of a per-profile file called base.
func (s *sessions) file(base string) string {
	if s.profile.Name != "" {
		base += "-" + unsafeName.ReplaceAllString(s.profile.Name, "_")
	}
	return filepath.Join(s.dir, base)
}

func (s *sessions) path() string {
	return s.file("session")
}

func (s *sessions) args(args ...string) []string {
	if s.profile.Account != "" {
		args = append(args, "--account", s.profile.Account)
	}
	return args
}

// get returns a session that works, for the shell.
func (s *sessions) get(ctx context.Context) (sessionVar, error) {
	if _, err := s.run(ctx, "vault", "list", "--format=json"); err != nil {
		return sessionVar{}, err
	}
	v, ok := s.load() // run saved the session that worked
	if !ok {
		return sessionVar{}, errors.New("no saved session")
	}
	return v, nil
}

// run runs op with the saved session if op whoami, a local check with no
// network call, accepts it, or else signs in first. whoami does not see a
// session that was signed out on the server, so when the command itself
// says it is not signed in, run signs in and runs it once more.
func (s *sessions) run(ctx context.Context, args ...string) ([]byte, error) {
	v, saved := s.load()
	if saved {
		if _, err := s.op(ctx, opCall{args: s.args("whoami"), session: v}); err != nil {
			saved = false
		}
	}
	if !saved {
		var err error
		if v, err = s.renew(ctx); err != nil {
			return nil, err
		}
	}
	out, err := s.op(ctx, opCall{args: s.args(args...), session: v})
	if err != nil && saved && notSignedIn(err) {
		if v, err = s.renew(ctx); err != nil {
			return nil, err
		}
		out, err = s.op(ctx, opCall{args: s.args(args...), session: v})
	}
	if err != nil {
		return nil, fmt.Errorf("op %s: %w", strings.Join(args[:min(2, len(args))], " "), err)
	}
	return out, nil
}

func notSignedIn(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"not currently signed in", "not signed in", "session expired", "invalid session", "authentication required"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (s *sessions) renew(ctx context.Context) (sessionVar, error) {
	v, err := s.signin(ctx)
	if err != nil {
		return v, err
	}
	return v, s.save(v)
}

func (s *sessions) load() (sessionVar, bool) {
	b, err := os.ReadFile(s.path())
	if err != nil {
		return sessionVar{}, false
	}
	m := exportRe.FindSubmatch(b)
	if m == nil {
		return sessionVar{}, false
	}
	return sessionVar{string(m[1]), string(m[2])}, true
}

func (s *sessions) save(v sessionVar) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".session-*") // mode 0600
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := io.WriteString(f, v.export()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path())
}

func (s *sessions) signin(ctx context.Context) (sessionVar, error) {
	call := opCall{args: s.args("signin")}
	switch {
	case len(s.profile.PasswordCommand) > 0:
		out, err := runCommand(ctx, s.profile.PasswordCommand)
		if err != nil {
			return sessionVar{}, fmt.Errorf("password_command: %w", err)
		}
		password, _, _ := strings.Cut(string(out), "\n")
		password = strings.TrimSuffix(password, "\r")
		if password == "" {
			return sessionVar{}, errors.New("password_command: no password on the first line")
		}
		call.stdin = strings.NewReader(password + "\n")
	case s.terminal:
		call.interactive = true
	case s.ask != nil:
		password, err := s.ask(ctx)
		if err != nil {
			return sessionVar{}, err
		}
		call.stdin = strings.NewReader(password + "\n")
	default:
		return sessionVar{}, errors.New("signin: no password_command configured and no terminal to ask on")
	}

	out, err := s.op(ctx, call)
	if err != nil {
		return sessionVar{}, fmt.Errorf("op signin: %w", err)
	}
	// Never quote out in an error: it holds the token.
	m := exportRe.FindSubmatch(out)
	if m == nil {
		return sessionVar{}, errors.New("op signin: no session in its output")
	}
	return sessionVar{string(m[1]), string(m[2])}, nil
}

func runOp(ctx context.Context, c opCall) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "op", c.args...)
	if c.session != (sessionVar{}) {
		cmd.Env = append(os.Environ(), c.session.env())
	}
	var stderr bytes.Buffer
	if c.interactive {
		cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	} else {
		cmd.Stdin, cmd.Stderr = c.stdin, &stderr
	}
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

// runCommand runs a password command, which may ask for a passphrase on
// the terminal.
func runCommand(ctx context.Context, argv []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	return cmd.Output()
}
