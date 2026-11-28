package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// copyToClipboard hands value to xclip, which stays in the background to
// own the selection. Its output goes to /dev/null so nothing waits on it.
func copyToClipboard(value string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i")
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}

func typeText(value string) error {
	if err := waitForModifierRelease(5 * time.Second); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond) // let focus return from rofi
	cmd := exec.Command("xdotool", "type", "--file", "-")
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}

// waitForModifierRelease waits until no modifier key (Ctrl from
// Ctrl+Enter, Shift, Alt, ...) is physically held, so typed text does not
// turn into shortcuts. Faking key releases does not work: auto-repeat
// presses a held key again.
func waitForModifierRelease(timeout time.Duration) error {
	conn, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer conn.Close()
	mm, err := xproto.GetModifierMapping(conn).Reply()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		km, err := xproto.QueryKeymap(conn).Reply()
		if err != nil {
			return err
		}
		if !anyHeld(km.Keys, mm.Keycodes) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("a modifier key is still held")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// anyHeld reports whether a key in codes is down in keys, the bitmap of
// QueryKeymap. Code 0 is an unused slot of the modifier mapping.
func anyHeld(keys []byte, codes []xproto.Keycode) bool {
	for _, c := range codes {
		if c != 0 && int(c/8) < len(keys) && keys[c/8]&(1<<(c%8)) != 0 {
			return true
		}
	}
	return false
}

func notify(urgent bool, msg string) {
	urgency := "low"
	if urgent {
		urgency = "critical"
	}
	exec.Command("notify-send", "-a", "opx", "-u", urgency, "opx", msg).Run()
}

// copySecret copies value like pass -c does: after a while, if the
// clipboard still holds value, it puts back what the clipboard held before.
func copySecret(value string, after time.Duration) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// A restore still waiting would save value as what to put back later.
	finishRestores(exe)
	before, _ := exec.Command("xclip", "-selection", "clipboard", "-o").Output() // empty or not text: nothing to restore
	if err := copyToClipboard(value); err != nil {
		return err
	}
	return startRestore(exe, clipboardRestore{Secret: value, Before: before}, after)
}

// finishRestores sends SIGTERM to the restores still waiting, which makes
// them put the clipboard back now, and waits up to a second for them to
// exit.
func finishRestores(exe string) {
	dirs, _ := os.ReadDir("/proc")
	var pids []int
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		b, _ := os.ReadFile("/proc/" + d.Name() + "/cmdline")
		args := strings.Split(string(b), "\x00")
		if len(args) > 1 && args[0] == exe && args[1] == "clipboard-restore" && syscall.Kill(pid, syscall.SIGTERM) == nil {
			pids = append(pids, pid)
		}
	}
	deadline := time.Now().Add(time.Second)
	for _, pid := range pids {
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// clipboardRestore is what opx clipboard-restore reads on stdin.
type clipboardRestore struct {
	Secret string
	Before []byte
}

// startRestore starts opx clipboard-restore in its own session, with r on
// stdin rather than in its arguments.
func startRestore(exe string, r clipboardRestore, after time.Duration) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "clipboard-restore", after.String())
	cmd.Stdin = pr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	pr.Close()
	if err != nil {
		pw.Close()
		return err
	}
	err = json.MarshalWrite(pw, r)
	pw.Close()
	cmd.Process.Release()
	return err
}

// restoreClipboard reads a clipboardRestore from stdin, waits, and puts
// back the clipboard if it still holds the secret. SIGTERM ends the wait
// early, as finishRestores does.
func restoreClipboard(after string) error {
	d, err := time.ParseDuration(after)
	if err != nil {
		return err
	}
	var r clipboardRestore
	if err := json.UnmarshalRead(os.Stdin, &r); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	sleepCtx(ctx, d)
	current, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil || string(current) != r.Secret {
		return nil // something else was copied since
	}
	return copyToClipboard(string(r.Before))
}
