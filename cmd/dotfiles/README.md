# dotfiles-go

A tool to manage and synchronize dotfiles from multiple repositories. It
automates the process of creating symlinks, configuring git, and running setup
scripts.

## Repository Discovery

By default, the tool looks for repositories in `~/src/dotfiles`. Each directory
in this path is treated as a separate dotfiles repository. This path can be
changed using the `-repos` flag or the `DOTFILES_REPOS` environment variable.

## dotfiles.toml Configuration

Each repository can contain a `dotfiles.toml` or `.dotfiles.toml` file to
configure its behavior.

### Ignore

Specify patterns for files and directories that should not be synchronized.

```toml
ignore = ["*.log", "temp/", "local-config"]
```

### Mount

Explicitly map source files or directories in the repository to specific
destinations in the target path. Supports wildcards and conditional execution.

```toml
[[mount]]
src = "config/awesome"
dst = ".config/awesome"

[[mount]]
condition = "os == 'linux'"
src = "bin/*"
dst = "bin"
```

When using wildcards in `src`, the files are placed inside the directory
specified by `dst`.

### Git Configuration

Set global git configuration values when the repository is synchronized.

```toml
[git]
"user.name" = "Your Name"
"user.email" = "user@example.com"
```

To unset a git configuration value, set it to an empty string.

### Scripts

Run shell scripts at different stages of the synchronization process. Scripts
are executed using an embedded shell interpreter.

- **phase = "pre"**: Runs before any other actions in the repository.
- **phase = "default"** (or empty): Runs after all other actions in the repository have been planned
  or executed.
- **phase = "post"**: Runs after all other actions in the repository have been
  completed.

```toml
[[script]]
phase = "pre"
src = "echo Starting sync..."

[[script]]
condition = "os == 'darwin'"
src = "echo Syncing on macOS..."

[[script]]
phase = "post"
src = "echo Sync complete."
```

### Conditions

The `condition` field in `[[mount]]` and `[[script]]` uses
[expr](github.com/expr-lang/expr) to determine if the action should be
performed. The current operating system is available as the `os` variable and
the system hostname as `hostname`.

### Default Root Item Handling

Files and directories at the root of the repository that are not ignored or
explicitly mounted are automatically symlinked to the destination path. If a
file name does not start with a dot, one is prepended (e.g., `bashrc` becomes
`~/.bashrc`).
