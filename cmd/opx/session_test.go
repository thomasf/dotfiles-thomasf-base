package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	fakeToken    = "FAKEtoken_0123-abc"
	fakeVar      = "OP_SESSION_FAKEUSERID"
	signinOutput = "export " + fakeVar + `="` + fakeToken + `"` + "\n" +
		"# This command is meant to be used with your shell's eval function.\n"
	notSignedInMsg = "[ERROR] 2026/11/28 11:06:08 You are not currently signed in. Please run `op signin --help` for instructions"
)

// fakeOp answers op calls. Commands work with the token in valid; whoami
// also accepts the token in whoamiOnly, like a session that was signed out.
type fakeOp struct {
	valid        string
	whoamiOnly   string
	cmdErr       error // commands fail with this
	signinOutput string
	calls        []opCall
	stdin        []string
}

func (f *fakeOp) has(c opCall, token string) bool {
	return token != "" && c.session == sessionVar{fakeVar, token}
}

func (f *fakeOp) run(_ context.Context, c opCall) ([]byte, error) {
	f.calls = append(f.calls, c)
	in := ""
	if c.stdin != nil {
		b, _ := io.ReadAll(c.stdin)
		in = string(b)
	}
	f.stdin = append(f.stdin, in)
	switch c.args[0] {
	case "whoami":
		if f.has(c, f.valid) || f.has(c, f.whoamiOnly) {
			return []byte("ok"), nil
		}
		return nil, errors.New("exit status 1: " + notSignedInMsg)
	case "signin":
		f.valid = fakeToken
		return []byte(f.signinOutput), nil
	case "vault", "item":
		if f.cmdErr != nil {
			return nil, f.cmdErr
		}
		if !f.has(c, f.valid) {
			return nil, errors.New("exit status 1: " + notSignedInMsg)
		}
		return []byte("[]"), nil
	}
	return nil, errors.New("unexpected op call")
}

func (f *fakeOp) commands() []string {
	var cmds []string
	for _, c := range f.calls {
		cmds = append(cmds, strings.Join(c.args, " "))
	}
	return cmds
}

func newTestSessions(t *testing.T, profile Profile, op *fakeOp) *sessions {
	t.Helper()
	return &sessions{
		profile: profile,
		dir:     filepath.Join(t.TempDir(), "opx"),
		op:      op.run,
	}
}

var withCommand = Profile{Name: "example", Account: "example", PasswordCommand: []string{"printf", `fake-password\nusername: someone\n`}}

const (
	callWhoami    = "whoami --account example"
	callSignin    = "signin --account example"
	callVaultList = "vault list --format=json --account example"
	callItemList  = "item list --format=json --account example"
)

func checkCalls(t *testing.T, op *fakeOp, want ...string) {
	t.Helper()
	if got := op.commands(); !slices.Equal(got, want) {
		t.Errorf("op calls\n%q\nwant\n%q", got, want)
	}
}

func TestSessionSigninAndSave(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, withCommand, op)

	v, err := s.get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v != (sessionVar{fakeVar, fakeToken}) {
		t.Errorf("got %+v", v)
	}
	checkCalls(t, op, callSignin, callVaultList)
	if op.stdin[0] != "fake-password\n" {
		t.Errorf("signin stdin %q, want only the first line", op.stdin[0])
	}

	path := filepath.Join(s.dir, "session-example")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "export " + fakeVar + `="` + fakeToken + `"` + "\n"; string(b) != want {
		t.Errorf("session file %q", b)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, s.dir: 0o700} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v (err %v)", p, fi.Mode().Perm(), want, err)
		}
	}
}

func TestSessionReusesValid(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, withCommand, op)
	if _, err := s.get(t.Context()); err != nil {
		t.Fatal(err)
	}
	op.calls = nil

	if _, err := s.run(t.Context(), "item", "list", "--format=json"); err != nil {
		t.Fatal(err)
	}
	checkCalls(t, op, callWhoami, callItemList)
}

func TestSessionExpired(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, withCommand, op)
	if err := s.save(sessionVar{fakeVar, "expiredToken"}); err != nil {
		t.Fatal(err)
	}

	v, err := s.get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Token != fakeToken {
		t.Errorf("token %q, want a new one", v.Token)
	}
	checkCalls(t, op, callWhoami, callSignin, callVaultList)
	if got, _ := s.load(); got.Token != fakeToken {
		t.Errorf("saved token %q", got.Token)
	}
}

// A signed out session passes whoami, and only the command itself fails.
func TestSessionSignedOut(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput, whoamiOnly: "signedOutToken"}
	s := newTestSessions(t, withCommand, op)
	if err := s.save(sessionVar{fakeVar, "signedOutToken"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.run(t.Context(), "item", "list", "--format=json"); err != nil {
		t.Fatal(err)
	}
	checkCalls(t, op, callWhoami, callItemList, callSignin, callItemList)
	if got, _ := s.load(); got.Token != fakeToken {
		t.Errorf("saved token %q", got.Token)
	}
}

func TestSessionOtherError(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, withCommand, op)
	if _, err := s.get(t.Context()); err != nil {
		t.Fatal(err)
	}
	op.calls = nil
	op.cmdErr = errors.New("exit status 1: [ERROR] item not found")

	_, err := s.run(t.Context(), "item", "get", "itemid", "--format=json")
	if err == nil || err.Error() != "op item get: exit status 1: [ERROR] item not found" {
		t.Errorf("err %v", err)
	}
	checkCalls(t, op, callWhoami, "item get itemid --format=json --account example")
}

func TestSessionNoAccount(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, Profile{PasswordCommand: withCommand.PasswordCommand}, op)
	if _, err := s.get(t.Context()); err != nil {
		t.Fatal(err)
	}
	checkCalls(t, op, "signin", "vault list --format=json")
	if filepath.Base(s.path()) != "session" {
		t.Errorf("path %s", s.path())
	}
}

func TestSessionProfileFileName(t *testing.T) {
	s := &sessions{profile: Profile{Name: "my.example.com/../x"}, dir: "/d"}
	if got := s.path(); got != "/d/session-my.example.com_.._x" {
		t.Errorf("path %s", got)
	}
}

func TestSessionTerminal(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, Profile{}, op)
	s.terminal = true
	if _, err := s.get(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c := op.calls[0]; c.args[0] != "signin" || !c.interactive || c.stdin != nil {
		t.Errorf("want an interactive signin, got %+v", c)
	}
}

func TestSessionAsk(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, Profile{}, op)
	s.ask = func(context.Context) (string, error) { return "asked-password", nil }
	if _, err := s.get(t.Context()); err != nil {
		t.Fatal(err)
	}
	if op.stdin[0] != "asked-password\n" {
		t.Errorf("signin stdin %q", op.stdin[0])
	}
}

func TestSessionNoPasswordSource(t *testing.T) {
	op := &fakeOp{signinOutput: signinOutput}
	s := newTestSessions(t, Profile{}, op)
	if _, err := s.get(t.Context()); err == nil || !strings.Contains(err.Error(), "no password_command") {
		t.Errorf("err %v", err)
	}
	if len(op.calls) != 0 {
		t.Errorf("op called: %q", op.commands())
	}
}

func TestSessionBadSigninOutput(t *testing.T) {
	for name, out := range map[string]string{
		"empty":     "",
		"injection": `export OP_SESSION_X="tok"; rm -rf ~` + "\n",
		"quote":     `export OP_SESSION_X="to"k"` + "\n",
		"bad name":  `export OP_SESSION_X-Y="tok"` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestSessions(t, withCommand, &fakeOp{signinOutput: out})
			_, err := s.get(t.Context())
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), "tok") {
				t.Errorf("error quotes the output: %v", err)
			}
			if _, err := os.Stat(s.path()); err == nil {
				t.Error("session file written")
			}
		})
	}
}
