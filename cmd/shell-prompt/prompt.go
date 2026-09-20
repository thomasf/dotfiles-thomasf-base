package main

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ANSI color sequences
const (
	colorReset = "\033[0m"

	// Regular colors
	colorBlack   = "\033[0;30m"
	colorRed     = "\033[0;31m"
	colorGreen   = "\033[0;32m"
	colorYellow  = "\033[0;33m"
	colorBlue    = "\033[0;34m"
	colorMagenta = "\033[0;35m"
	colorCyan    = "\033[0;36m"
	colorWhite   = "\033[0;37m"

	// High intensity colors
	colorBlackH   = "\033[0;90m"
	colorRedH     = "\033[0;91m"
	colorGreenH   = "\033[0;92m"
	colorYellowH  = "\033[0;93m"
	colorBlueH    = "\033[0;94m"
	colorMagentaH = "\033[0;95m"
	colorCyanH    = "\033[0;96m"
	colorWhiteH   = "\033[0;97m"

	// Background colors
	colorBlackB   = "\033[40m"
	colorRedB     = "\033[41m"
	colorGreenB   = "\033[42m"
	colorYellowB  = "\033[43m"
	colorBlueB    = "\033[44m"
	colorMagentaB = "\033[45m"
	colorCyanB    = "\033[46m"
	colorWhiteB   = "\033[47m"
)

// ShellType represents the target shell syntax for zero-width escape wrapping.
type ShellType string

const (
	ShellBash  ShellType = "bash"
	ShellZsh   ShellType = "zsh"
	ShellPlain ShellType = "plain"
)

// Escape wraps non-printing ANSI escapes for the appropriate shell.
func (s ShellType) Escape(seq string) string {
	if seq == "" {
		return ""
	}
	switch s {
	case ShellBash:
		return "\\[" + seq + "\\]"
	case ShellZsh:
		return "%{" + seq + "%}"
	case ShellPlain:
		return seq
	default:
		return "\\[" + seq + "\\]"
	}
}

// Config holds all parameters needed to build the prompt.
type Config struct {
	Shell      ShellType
	ReturnCode int
	Cols       int
	ShowTitle  bool
	Dir        string
	HomeDir    string
	User       string
	Host       string
	HostTags   string
	VirtualEnv string
	RVM        string
	SSHClient  string
	EUID       int
	Time       time.Time
	VCS        string
}

// ResolveDefaultConfig inspects the environment and system state to populate Config defaults.
func ResolveDefaultConfig() Config {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	term := os.Getenv("TERM")
	showTitle := false
	if strings.HasPrefix(term, "xterm") || strings.HasPrefix(term, "rxvt") || strings.HasPrefix(term, "alacritty") {
		showTitle = true
	}

	ssh := os.Getenv("SSH_CLIENT")
	if ssh == "" {
		ssh = os.Getenv("SSH_TTY")
	}
	if ssh == "" {
		ssh = os.Getenv("SSH_CONNECTION")
	}

	return Config{
		Shell:      ShellBash,
		ReturnCode: 0,
		Cols:       GetTerminalCols(),
		ShowTitle:  showTitle,
		Dir:        cwd,
		HomeDir:    home,
		User:       ResolveUsername(),
		Host:       ResolveHostname(),
		HostTags:   os.Getenv("HOST_TAGS"),
		VirtualEnv: os.Getenv("VIRTUAL_ENV"),
		RVM:        os.Getenv("RVM"),
		SSHClient:  ssh,
		EUID:       os.Geteuid(),
		Time:       time.Now(),
	}
}

// ResolveUsername resolves username or alias, cut to 4 characters if no alias is set.
func ResolveUsername() string {
	if alias := os.Getenv("USER_ALIAS"); alias != "" {
		return alias
	}
	if alias := os.Getenv("__USER_ALIAS"); alias != "" {
		return alias
	}

	name := os.Getenv("USER")
	if name == "" {
		name = os.Getenv("LOGNAME")
	}
	if name == "" {
		if u, err := user.Current(); err == nil {
			name = u.Username
			// In case of domain\user or user@domain
			if idx := strings.LastIndex(name, "\\"); idx != -1 {
				name = name[idx+1:]
			}
		}
	}

	return cutRunePrefix(name, 4)
}

// ResolveHostname resolves hostname or alias, cut to 4 characters if no alias is set.
func ResolveHostname() string {
	if alias := os.Getenv("HOST_ALIAS"); alias != "" {
		return alias
	}

	h, err := os.Hostname()
	if err != nil {
		h = os.Getenv("HOSTNAME")
	}

	return cutRunePrefix(h, 4)
}

func cutRunePrefix(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\a\x1b]*(\a|\x1b\\)|\x01|\x02|\\\[|\\\]|%\{|%\}`)

// VisualLen returns the number of visible terminal columns used by a string.
func VisualLen(s string) int {
	stripped := ansiRegex.ReplaceAllString(s, "")
	return utf8.RuneCountInString(stripped)
}

// BuildPrompt generates the full prompt string matching config-base/bash/prompt.
func BuildPrompt(cfg Config, vcs VCSProvider) string {
	w := cfg.Cols
	if w <= 0 {
		w = 100
	}

	t := cfg.Time
	if t.IsZero() {
		t = time.Now()
	}
	hh := t.Format("15") // 24h format %02d
	mm := t.Format("04") // minute %02d

	uname := cfg.User
	hname := cfg.Host

	ssh := ""
	if cfg.SSHClient != "" {
		if cfg.Shell == ShellBash {
			// In bash prompt strings, \ must be escaped as \\
			ssh = "/\\\\"
		} else {
			ssh = "/\\"
		}
	}

	// EUID and ReturnCode determine LAST and LASTCOLOR
	var lastColor string
	var last string
	if cfg.EUID == 0 {
		if cfg.ReturnCode == 0 {
			lastColor = cfg.Shell.Escape(colorYellow)
			last = "#"
		} else {
			lastColor = cfg.Shell.Escape(colorRedH)
			last = "!#"
		}
	} else {
		if cfg.ReturnCode == 0 {
			lastColor = cfg.Shell.Escape(colorYellowH)
			last = "$"
		} else {
			lastColor = cfg.Shell.Escape(colorRedH)
			last = "!$"
		}
	}

	// Python virtual environment
	venv := ""
	if cfg.VirtualEnv != "" {
		venv = " p:" + filepath.Base(cfg.VirtualEnv)
	}

	// Version control string
	vcsStr := cfg.VCS
	if vcsStr == "" && vcs != nil {
		vcsStr = vcs.GetVCS(cfg.Dir, cfg.HostTags)
	}

	// Working directory compaction:
	// pwdw = w - PADDING - ${#LAST} - ${#VCS} - ${#VENV} - ${#RVM}
	padding := 5
	vcsLen := VisualLen(vcsStr)
	pwdw := max(w-padding-utf8.RuneCountInString(last)-vcsLen-utf8.RuneCountInString(venv)-utf8.RuneCountInString(cfg.RVM), 10)
	pwd := DirChomp(cfg.Dir, pwdw, cfg.HomeDir)

	// Host color based on HOST_TAGS
	var hostColor string
	if strings.Contains(cfg.HostTags, ":server:") {
		hostColor = cfg.Shell.Escape(colorBlack) + cfg.Shell.Escape(colorBlueB)
	} else if strings.Contains(cfg.HostTags, ":workstation:") {
		hostColor = cfg.Shell.Escape(colorCyan)
	} else {
		hostColor = cfg.Shell.Escape(colorWhite) + cfg.Shell.Escape(colorBlackB)
	}

	// Titlebar
	titlebar := ""
	if cfg.ShowTitle {
		titlePwd := DirChomp(cfg.Dir, 40, cfg.HomeDir)
		shellName := "bash"
		if cfg.Shell == ShellZsh {
			shellName = "zsh"
		}
		titleSeq := "\033]0;" + shellName + ": " + uname + "@" + hname + ": " + titlePwd + "\007"
		titlebar = cfg.Shell.Escape(titleSeq)
	}

	// Assemble PS1:
	// Line 1:
	// ${TITLEBAR}${yellowH}▶${HH} ${blue}${PWD}${cyanH}${VCS}${yellowH}${VENV}${RVM}
	var sb strings.Builder
	sb.WriteString(titlebar)
	sb.WriteString(cfg.Shell.Escape(colorYellowH))
	sb.WriteString("▶")
	sb.WriteString(hh)
	sb.WriteString(" ")
	sb.WriteString(cfg.Shell.Escape(colorBlue))
	sb.WriteString(pwd)
	sb.WriteString(cfg.Shell.Escape(colorCyanH))
	if vcsStr != "" {
		sb.WriteString(vcsStr)
	}
	sb.WriteString(cfg.Shell.Escape(colorYellowH))
	if venv != "" {
		sb.WriteString(venv)
	}
	if cfg.RVM != "" {
		sb.WriteString(cfg.RVM)
	}

	// Line 2:
	// \n${yellowH}:${MM} ${UCOLOR}${UNAME}${yellowH}@${HOST_COLOR}${HNAME}${black}${cyanB}${SSH}${LASTCOLOR}${LAST} ${resetFormating}
	sb.WriteString("\n")
	sb.WriteString(cfg.Shell.Escape(colorYellowH))
	sb.WriteString(":")
	sb.WriteString(mm)
	sb.WriteString(" ")
	sb.WriteString(cfg.Shell.Escape(colorCyan)) // UCOLOR is cyan
	sb.WriteString(uname)
	sb.WriteString(cfg.Shell.Escape(colorYellowH))
	sb.WriteString("@")
	sb.WriteString(hostColor)
	sb.WriteString(hname)
	if ssh != "" {
		sb.WriteString(cfg.Shell.Escape(colorBlack))
		sb.WriteString(cfg.Shell.Escape(colorCyanB))
		sb.WriteString(ssh)
	}
	sb.WriteString(lastColor)
	sb.WriteString(last)
	sb.WriteString(" ")
	sb.WriteString(cfg.Shell.Escape(colorReset))

	return sb.String()
}
