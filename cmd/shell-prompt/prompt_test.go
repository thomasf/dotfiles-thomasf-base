package main

import (
	"strings"
	"testing"
	"time"
)

type mockVCS struct {
	result string
}

func (m *mockVCS) GetVCS(dir, hostTags string) string {
	return m.result
}

func TestBuildPrompt(t *testing.T) {
	fixedTime := time.Date(2026, 9, 18, 9, 25, 0, 0, time.UTC)

	baseCfg := Config{
		Shell:      ShellBash,
		ReturnCode: 0,
		Cols:       100,
		ShowTitle:  false,
		Dir:        "/home/t/src/dotfiles/base",
		HomeDir:    "/home/t",
		User:       "t",
		Host:       "flam",
		HostTags:   ":workstation:",
		VirtualEnv: "",
		RVM:        "",
		SSHClient:  "",
		EUID:       1000,
		Time:       fixedTime,
		VCS:        "",
	}

	t.Run("basic user workstation prompt", func(t *testing.T) {
		prompt := BuildPrompt(baseCfg, &mockVCS{result: " (master)"})

		// Line 1 checks:
		// starts with ▶09, blue path, cyanH VCS
		if !strings.Contains(prompt, "▶09 ") {
			t.Errorf("prompt missing ▶09: %s", prompt)
		}
		if !strings.Contains(prompt, "\\[\033[0;34m\\]~/src/dotfiles/base") {
			t.Errorf("prompt missing blue dir: %s", prompt)
		}
		if !strings.Contains(prompt, "\\[\033[0;96m\\] (master)") {
			t.Errorf("prompt missing vcs: %s", prompt)
		}

		// Line 2 checks:
		// :25 cyan t yellowH @ cyan flam yellowH $
		if !strings.Contains(prompt, "\n\\[\033[0;93m\\]:25 ") {
			t.Errorf("prompt missing :25: %s", prompt)
		}
		if !strings.Contains(prompt, "\\[\033[0;36m\\]t\\[\033[0;93m\\]@\\[\033[0;36m\\]flam") {
			t.Errorf("prompt missing user@host: %s", prompt)
		}
		if !strings.Contains(prompt, "\\[\033[0;93m\\]$ \\[\033[0m\\]") {
			t.Errorf("prompt missing yellowH $ reset: %s", prompt)
		}
	})

	t.Run("root user with error code", func(t *testing.T) {
		cfg := baseCfg
		cfg.EUID = 0
		cfg.ReturnCode = 1

		prompt := BuildPrompt(cfg, &mockVCS{result: ""})
		// Root with error should have redH "!#"
		if !strings.Contains(prompt, "\\[\033[0;91m\\]!# \\[\033[0m\\]") {
			t.Errorf("prompt missing root error !#: %s", prompt)
		}
	})

	t.Run("root user success", func(t *testing.T) {
		cfg := baseCfg
		cfg.EUID = 0
		cfg.ReturnCode = 0

		prompt := BuildPrompt(cfg, &mockVCS{result: ""})
		// Root with success should have yellow "#"
		if !strings.Contains(prompt, "\\[\033[0;33m\\]# \\[\033[0m\\]") {
			t.Errorf("prompt missing root success #: %s", prompt)
		}
	})

	t.Run("non-root user with error code", func(t *testing.T) {
		cfg := baseCfg
		cfg.EUID = 1000
		cfg.ReturnCode = 127

		prompt := BuildPrompt(cfg, &mockVCS{result: ""})
		// Non-root error should have redH "!$"
		if !strings.Contains(prompt, "\\[\033[0;91m\\]!$ \\[\033[0m\\]") {
			t.Errorf("prompt missing user error !$: %s", prompt)
		}
	})

	t.Run("server host color", func(t *testing.T) {
		cfg := baseCfg
		cfg.HostTags = ":server:production:"

		prompt := BuildPrompt(cfg, nil)
		// Server: black on blue background
		if !strings.Contains(prompt, "\\[\033[0;30m\\]\\[\033[44m\\]flam") {
			t.Errorf("prompt missing server host color: %s", prompt)
		}
	})

	t.Run("default host color", func(t *testing.T) {
		cfg := baseCfg
		cfg.HostTags = ""

		prompt := BuildPrompt(cfg, nil)
		// Default: white on black background
		if !strings.Contains(prompt, "\\[\033[0;37m\\]\\[\033[40m\\]flam") {
			t.Errorf("prompt missing default host color: %s", prompt)
		}
	})

	t.Run("ssh client indicator in bash", func(t *testing.T) {
		cfg := baseCfg
		cfg.SSHClient = "192.168.1.100 52341 22"

		prompt := BuildPrompt(cfg, nil)
		// SSH indicator: black on cyan background with /\\
		if !strings.Contains(prompt, "\\[\033[0;30m\\]\\[\033[46m\\]/\\\\") {
			t.Errorf("prompt missing SSH indicator in bash: %s", prompt)
		}
	})

	t.Run("ssh client indicator in plain", func(t *testing.T) {
		cfg := baseCfg
		cfg.Shell = ShellPlain
		cfg.SSHClient = "192.168.1.100 52341 22"

		prompt := BuildPrompt(cfg, nil)
		if !strings.Contains(prompt, "\033[0;30m\033[46m/\\") {
			t.Errorf("prompt missing SSH indicator in plain: %s", prompt)
		}
	})

	t.Run("virtualenv and rvm", func(t *testing.T) {
		cfg := baseCfg
		cfg.VirtualEnv = "/home/t/.virtualenvs/myproject"
		cfg.RVM = " ruby-3.2.0"

		prompt := BuildPrompt(cfg, nil)
		if !strings.Contains(prompt, " p:myproject") {
			t.Errorf("prompt missing virtualenv: %s", prompt)
		}
		if !strings.Contains(prompt, " ruby-3.2.0") {
			t.Errorf("prompt missing RVM: %s", prompt)
		}
	})

	t.Run("xterm titlebar in bash", func(t *testing.T) {
		cfg := baseCfg
		cfg.ShowTitle = true

		prompt := BuildPrompt(cfg, nil)
		expectedTitle := "\\[\033]0;bash: t@flam: ~/src/dotfiles/base\007\\]"
		if !strings.HasPrefix(prompt, expectedTitle) {
			t.Errorf("prompt missing titlebar prefix: %s", prompt)
		}
	})

	t.Run("zsh escaping", func(t *testing.T) {
		cfg := baseCfg
		cfg.Shell = ShellZsh
		cfg.ShowTitle = true

		prompt := BuildPrompt(cfg, nil)
		if !strings.HasPrefix(prompt, "%{\033]0;zsh: t@flam: ~/src/dotfiles/base\007%}") {
			t.Errorf("zsh prompt title prefix incorrect: %s", prompt)
		}
		if !strings.Contains(prompt, "%{\033[0;93m%}") {
			t.Errorf("zsh prompt color escaping incorrect: %s", prompt)
		}
	})
}
