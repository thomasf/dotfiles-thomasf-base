package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRofi puts a rofi first on PATH that prints out, prints errOut on
// stderr and exits with code.
func fakeRofi(t *testing.T, out, errOut, code string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "rofi")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s' \"$FAKE_OUT\"\nprintf '%s' \"$FAKE_ERR\" >&2\nexit \"$FAKE_CODE\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_OUT", out)
	t.Setenv("FAKE_ERR", errOut)
	t.Setenv("FAKE_CODE", code)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRofiMenu(t *testing.T) {
	req := menuRequest{Prompt: "p", Lines: []string{"a", "b", "c"}}
	for name, tc := range map[string]struct {
		out, errOut, code string
		i                 int
		act               action
		err               string
	}{
		"enter":         {out: "1\n", code: "0", i: 1, act: actCopy},
		"ctrl+enter":    {out: "2\n", code: "10", i: 2, act: actType},
		"escape":        {code: "1", err: "cancelled"},
		"no match":      {out: "-1\n", code: "0", err: "cancelled"},
		"other failure": {errOut: "boom\n", code: "2", err: "rofi: exit status 2: boom"},
	} {
		t.Run(name, func(t *testing.T) {
			fakeRofi(t, tc.out, tc.errOut, tc.code)
			i, act, err := rofiMenu(t.Context(), req)
			if tc.err != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.err) {
					t.Errorf("err %v, want prefix %q", err, tc.err)
				}
				if tc.err == "cancelled" && !errors.Is(err, errCancelled) {
					t.Errorf("err %v is not errCancelled", err)
				}
				return
			}
			if err != nil || i != tc.i || act != tc.act {
				t.Errorf("got %d %v %v, want %d %v", i, act, err, tc.i, tc.act)
			}
		})
	}
}

func TestRofiPassword(t *testing.T) {
	fakeRofi(t, "pa ss\n", "", "0")
	if got, err := rofiPassword(t.Context()); err != nil || got != "pa ss" {
		t.Errorf("got %q %v", got, err)
	}
}
