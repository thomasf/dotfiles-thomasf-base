package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func checkDisplay() error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("rofi: no display (DISPLAY is not set)")
	}
	return nil
}

// runRofi runs rofi. Exit code 1 is Escape, 10 is the custom key.
func runRofi(ctx context.Context, args []string, stdin string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, "rofi", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, 0, nil
	}
	ee, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return nil, 0, err
	}
	switch ee.ExitCode() {
	case 1:
		return nil, 1, errCancelled
	case 10:
		return out, 10, nil
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return nil, 0, fmt.Errorf("rofi: %w: %s", err, msg)
	}
	return nil, 0, fmt.Errorf("rofi: %w", err)
}

// rofiMenu shows lines and returns the index of the chosen one.
func rofiMenu(ctx context.Context, r menuRequest) (int, action, error) {
	args := []string{"-dmenu", "-i", "-no-custom", "-format", "i", "-p", r.Prompt}
	if r.Selected > 0 {
		args = append(args, "-selected-row", strconv.Itoa(r.Selected))
	}
	if r.Message != "" {
		args = append(args, "-mesg", r.Message)
	}
	if r.TypeKey {
		args = append(args, "-kb-accept-custom", "", "-kb-custom-1", "Control+Return")
	}
	out, code, err := runRofi(ctx, args, strings.Join(r.Lines, "\n")+"\n")
	if err != nil {
		return 0, actCopy, err
	}
	act := actCopy
	if code == 10 {
		act = actType
	}
	i, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || i < 0 || i >= len(r.Lines) {
		return 0, act, errCancelled
	}
	return i, act, nil
}

// rofiPassword asks for the 1Password account password.
func rofiPassword(ctx context.Context) (string, error) {
	out, _, err := runRofi(ctx, []string{"-dmenu", "-password",
		"-p", "1Password password", "-theme-str", "listview { enabled: false; }"}, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}
