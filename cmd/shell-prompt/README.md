# shell-prompt

`shell-prompt` is a fast port of the bash prompt (`base/config-base/bash/prompt`) written in Go.

It replaces slow multi-process shell scripts (`whoami`, `hostname`, `date`, `pwd`, subshell forks) with a single, highly-optimized executable that computes the full prompt in ~3ms.

## Features

- **Exact bash prompt layout**: Matches the colors, unicode symbols (`▶`), and layout of `config-base/bash/prompt`.
- **`DirChomp` algorithm**: Faithfully reproduces the directory shortening algorithm (`__dir_chomp`) to fit the path into available terminal width.
- **Fast VCS integration**:
  - Automatically queries `vcprompt` if installed in `PATH`.
  - Seamless fallback to native Git status when `vcprompt` is not available.
  - Respects `:minimalshell:` in `HOST_TAGS` to disable VCS.
- **Dynamic host coloring**:
  - `HOST_TAGS` containing `:server:` displays black text on a blue background.
  - `HOST_TAGS` containing `:workstation:` displays cyan text.
  - Default displays white text on a black background.
- **Status symbol (`LAST` & `LASTCOLOR`)**:
  - Regular user: `$` (yellow) on success (`RET=0`), `!$` (red) on error (`RET!=0`).
  - Root user (`EUID=0`): `#` (yellow) on success, `!#` (red) on error.
- **SSH indicator**: Displays black on cyan background `/\` when connected via SSH (`SSH_CLIENT`, `SSH_TTY`, or `SSH_CONNECTION`).
- **Python virtualenv**: Shows ` p:<name>` when `VIRTUAL_ENV` is active.
- **Window title**: Emits xterm/rxvt terminal titlebar sequences (`\033]0;bash: <user>@<host>: <title_pwd>\007`) when running under compatible terminals.
- **Multi-shell support**: Supports `bash` (`\[...\]`), `zsh` (`%{...%}`), and `plain` escaping.

## Installation

Build and install to your `PATH` (e.g. `~/bin` or `/usr/local/bin`):

```bash
go build -o ~/bin/shell-prompt .
```

## Shell Integration

### Bash

Add to your `~/.bashrc` (or evaluate `shell-prompt init bash`):

```bash
__prompt_command() {
  local ret=$?
  PS1="$(shell-prompt --ret=$ret)"
}

export PROMPT_COMMAND="__prompt_command"
```

To include command history, dark mode switching, and the DEBUG titlebar trap identical to `config-base/bash/prompt`, run:

```bash
eval "$(shell-prompt init bash)"
```

### Zsh

Add to your `~/.zshrc`:

```zsh
eval "$(shell-prompt init zsh)"
```

## CLI Usage

```
shell-prompt [flags]
shell-prompt chomp <path> [max-width]
shell-prompt title-pwd [path]
shell-prompt username
shell-prompt hostname
shell-prompt vcs [dir]
shell-prompt init [bash|zsh]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--ret <int>` | `$RET` or `0` | Exit status code of previous command |
| `--cols <int>` | Detected / `100` | Terminal width in columns |
| `--shell <name>` | `bash` | Shell syntax (`bash`, `zsh`, `plain`) |
| `--title` | Auto (`xterm*`, `rxvt*`) | Force enable terminal title sequence |
| `--no-title` | | Force disable terminal title sequence |
| `--pwd <dir>` | Current directory | Working directory override |
| `--user <name>` | Current user / alias | Username override |
| `--host <name>` | Current host / alias | Hostname override |
| `--tags <tags>` | `$HOST_TAGS` | Host tags override |
| `--time <HH:MM>` | Current time | Time override |
| `--vcs <string>` | Auto-detected | VCS string override |
| `--no-vcs` | | Disable VCS detection |
| `--euid <int>` | `$EUID` | Effective UID override |
| `--eval` | `false` | Output as `PS1='...'` for shell `eval` |
