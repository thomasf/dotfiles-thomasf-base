package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "chomp":
			runChomp(os.Args[2:])
			return
		case "title-pwd":
			runTitlePwd(os.Args[2:])
			return
		case "username":
			runUsername(os.Args[2:])
			return
		case "hostname":
			runHostname(os.Args[2:])
			return
		case "vcs":
			runVCS(os.Args[2:])
			return
		case "init":
			runInit(os.Args[2:])
			return
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}

	runPrompt(os.Args[1:])
}

func printHelp() {
	fmt.Print(`shell-prompt - fast port of bash prompt in Go

Usage:
  shell-prompt [flags]
  shell-prompt chomp <path> [max-width]
  shell-prompt title-pwd [path]
  shell-prompt username
  shell-prompt hostname
  shell-prompt vcs [dir]
  shell-prompt init [bash|zsh]

Flags:
  --ret <int>        Exit code of last command (defaults to $RET or 0)
  --cols <int>       Terminal columns (defaults to auto-detected terminal width or $COLUMNS)
  --shell <name>     Shell syntax: bash (default), zsh, plain
  --title            Include terminal window title sequence (auto-detected if unset)
  --no-title         Disable terminal window title sequence
  --pwd <dir>        Working directory (defaults to current working directory)
  --user <name>      Override username
  --host <name>      Override hostname
  --tags <tags>      Override HOST_TAGS environment variable
  --time <HH:MM>     Override time
  --vcs <string>     Override VCS string
  --no-vcs           Disable VCS detection
  --euid <int>       Override effective user ID
  --eval             Format output as PS1='...' for shell eval
`)
}

func runPrompt(args []string) {
	fs := flag.NewFlagSet("shell-prompt", flag.ExitOnError)

	cfg := ResolveDefaultConfig()

	var (
		retFlag     int
		colsFlag    int
		shellFlag   string
		titleFlag   bool
		noTitleFlag bool
		pwdFlag     string
		userFlag    string
		hostFlag    string
		tagsFlag    string
		timeFlag    string
		vcsFlag     string
		noVCSFlag   bool
		euidFlag    int
		evalFlag    bool
	)

	// Check if $RET is set in environment for default return code
	defaultRet := 0
	if retEnv := os.Getenv("RET"); retEnv != "" {
		if r, err := strconv.Atoi(retEnv); err == nil {
			defaultRet = r
		}
	}

	fs.IntVar(&retFlag, "ret", defaultRet, "Exit code of last command")
	fs.IntVar(&colsFlag, "cols", 0, "Terminal width in columns")
	fs.StringVar(&shellFlag, "shell", "bash", "Target shell (bash, zsh, plain)")
	fs.BoolVar(&titleFlag, "title", false, "Force enable terminal titlebar")
	fs.BoolVar(&noTitleFlag, "no-title", false, "Force disable terminal titlebar")
	fs.StringVar(&pwdFlag, "pwd", "", "Working directory")
	fs.StringVar(&userFlag, "user", "", "Username override")
	fs.StringVar(&hostFlag, "host", "", "Hostname override")
	fs.StringVar(&tagsFlag, "tags", "", "HOST_TAGS override")
	fs.StringVar(&timeFlag, "time", "", "Time override (HH:MM)")
	fs.StringVar(&vcsFlag, "vcs", "", "VCS override")
	fs.BoolVar(&noVCSFlag, "no-vcs", false, "Disable VCS info")
	fs.IntVar(&euidFlag, "euid", -1, "Effective UID override")
	fs.BoolVar(&evalFlag, "eval", false, "Output as PS1='...' for eval")

	// Allow "prompt" or "print" as optional first argument
	if len(args) > 0 && (args[0] == "prompt" || args[0] == "print") {
		args = args[1:]
	}

	_ = fs.Parse(args)

	cfg.ReturnCode = retFlag

	if colsFlag > 0 {
		cfg.Cols = colsFlag
	}

	switch strings.ToLower(shellFlag) {
	case "zsh":
		cfg.Shell = ShellZsh
	case "plain":
		cfg.Shell = ShellPlain
	default:
		cfg.Shell = ShellBash
	}

	if noTitleFlag {
		cfg.ShowTitle = false
	} else if titleFlag {
		cfg.ShowTitle = true
	}

	if pwdFlag != "" {
		cfg.Dir = pwdFlag
	}
	if userFlag != "" {
		cfg.User = userFlag
	}
	if hostFlag != "" {
		cfg.Host = hostFlag
	}
	if tagsFlag != "" {
		cfg.HostTags = tagsFlag
	}
	if euidFlag >= 0 {
		cfg.EUID = euidFlag
	}

	if timeFlag != "" {
		parts := strings.Split(timeFlag, ":")
		if len(parts) == 2 {
			if h, err1 := strconv.Atoi(parts[0]); err1 == nil {
				if m, err2 := strconv.Atoi(parts[1]); err2 == nil {
					now := time.Now()
					cfg.Time = time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
				}
			}
		}
	}

	var vcsProvider VCSProvider
	if noVCSFlag {
		cfg.VCS = ""
	} else if vcsFlag != "" {
		cfg.VCS = vcsFlag
	} else {
		vcsProvider = &DefaultVCSProvider{Timeout: 500 * time.Millisecond}
	}

	prompt := BuildPrompt(cfg, vcsProvider)

	if evalFlag {
		// Escape single quotes for shell eval
		escaped := strings.ReplaceAll(prompt, "'", `'\''`)
		fmt.Printf("PS1='%s'\n", escaped)
	} else {
		fmt.Print(prompt)
	}
}

func runChomp(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: shell-prompt chomp <path> [max-width]")
		os.Exit(1)
	}
	path := args[0]
	maxLen := 40
	if len(args) > 1 {
		if w, err := strconv.Atoi(args[1]); err == nil && w > 0 {
			maxLen = w
		}
	}
	home, _ := os.UserHomeDir()
	fmt.Print(DirChomp(path, maxLen, home))
}

func runTitlePwd(args []string) {
	path := ""
	if len(args) > 0 && args[0] != "" {
		path = args[0]
	} else {
		path, _ = os.Getwd()
	}
	home, _ := os.UserHomeDir()
	fmt.Print(DirChomp(path, 40, home))
}

func runUsername(args []string) {
	fmt.Print(ResolveUsername())
}

func runHostname(args []string) {
	fmt.Print(ResolveHostname())
}

func runVCS(args []string) {
	dir := ""
	if len(args) > 0 {
		dir = args[0]
	} else {
		dir, _ = os.Getwd()
	}
	tags := os.Getenv("HOST_TAGS")
	p := &DefaultVCSProvider{Timeout: 500 * time.Millisecond}
	fmt.Print(p.GetVCS(dir, tags))
}

func runInit(args []string) {
	shell := "bash"
	if len(args) > 0 {
		shell = strings.ToLower(args[0])
	}

	switch shell {
	case "zsh":
		os.Stdout.WriteString(`__prompt_precmd() {
  local ret=$?
  PROMPT="$(shell-prompt --shell=zsh --ret=$ret)"
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd __prompt_precmd
`)
	case "bash":
		fallthrough
	default:
		os.Stdout.WriteString(`__prompt_command() {
  local ret=$?
  export __RUNNING_PROMPT_COMMAND=1
  case $TERM in
    dumb) ;;
    *)
      [ "$CONFIDENTAL" == "confidental" ] || __pwd_logger 2>/dev/null
      [ "$CONFIDENTAL" == "confidental" ] || history -a 2>/dev/null
      [ -n "${DARKMODE}" ] && __darkmode_switcher 2>/dev/null
      echo -ne '\a'
      ;;
  esac
  PS1="$(shell-prompt --ret=$ret)"
  [ "$CONFIDENTAL" == "confidental" ] || zzz --add "$(command pwd 2>/dev/null)" 2>/dev/null
  unset __RUNNING_PROMPT_COMMAND
}

__prompt_username() { shell-prompt username "$@"; }
__prompt_hostname() { shell-prompt hostname "$@"; }
__dir_chomp() { shell-prompt chomp "$@"; }
__title_pwd() { shell-prompt title-pwd "$@"; }

__prompt_activate() {
  case $TERM in
    dumb) return ;;
    xterm*|rxvt*)
      trap '
[[ ! $BASH_SOURCE ]] &&
[[ ! $COMP_LINE ]] &&
[[ ! $BASH_COMMAND == "export RET="* ]] &&
[[ ! $BASH_COMMAND == "__prompt_command" ]] &&
[[ ! $BASH_COMMAND == "export __RUNNING_PROMPT_COMMAND=1" ]] &&
[[ ! $__RUNNING_PROMPT_COMMAND ]] &&
printf "\e]0;%s\a" "bash: $(shell-prompt username)@$(shell-prompt hostname): $BASH_COMMAND $(shell-prompt title-pwd)" >/dev/tty' DEBUG
      ;;
  esac

  PS2='> '
  PS4='+ '
  export PROMPT_COMMAND="__prompt_command"
}

__prompt_activate
`)
	}
}
