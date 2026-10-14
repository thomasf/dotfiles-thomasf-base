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

- **phase = "pre"**: Runs before the files and git configuration of any
  repository are set up, after `go install`.
- **phase = "default"** (or empty): Runs after the repository's own files, git
  configuration and templates, before the next repository's.
- **phase = "post"**: Runs after every repository has been set up.

See [Order of Execution](#order-of-execution) for the full order.

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

### Repositories

Gnown dotfiles repositories and the git remotes each of them should
have. The section can appear in any number of repositories' config files:
`names` from all files are merged and deduplicated, and a remote may be defined
in several files as long as every definition is identical.

```toml
[repositories]
names = ["base", "emacs", "private"]

[repositories.remote.origin]
kind = "private"
url  = "ssh://git@example.com/dotfiles-{name}.git"

[repositories.remote."backup.disk"]
kind      = "backup"
url       = "/mnt/something/git/dotfiles/{name}.git"
condition = "hostname == 'myhost'"

[repositories.remote.publish]
kind    = "publish"
url     = "https://github.com/me/dotfiles-{name}"
pushurl = "git@github.com:me/dotfiles-{name}"
```

`{name}` in `url` and `pushurl` is replaced by the repository name. The
optional `pushurl` becomes git's `remote.<name>.pushurl`: git pushes to it and
fetches from `url`. `kind` decides how the remote is used:

- **private**: `pull` pulls from it and `push` pushes to it. At most one
  private remote can be active.
- **backup**: `push` pushes to it. If the backup is not reachable (a local path
  that does not exist, or a failed push), `push` prints a warning instead of
  failing.
- **publish**: `publish` pushes to it, but only from repositories whose own
  config sets `public = true`. A publish remote on any other repository is an
  error.

A remote whose `condition` is false is ignored on that host. Without any
configured remotes, `push`, `pull` and `publish` use the remotes named
`origin`, `backup.*` and `publish`.

The upstream remote, which `pull` and `repos clone` use, is the active private
remote or, if none is active, the active publish remote. Putting the public
repository names and the publish remote in a public repository's config
therefore lets anyone clone the public repositories with only that repository
checked out.

The `repos` commands use this configuration:

- `dotfiles repos`: show each known repository and the state of its remotes.
- `dotfiles repos fix`: add missing remotes, correct remote URLs, and create
  missing local backup repositories (`git init --bare`) when their parent
  directory exists. Remotes are never removed.
- `dotfiles repos clone [name...]`: clone the repositories in `names` that are
  missing from the repositories path, or only the named ones, from the upstream
  remote, then run `repos fix`.

Pass `-dry-run` before the command (`dotfiles -dry-run repos fix`) to print the
actions without running them.

### Templates

Render a Go [text/template](https://pkg.go.dev/text/template) file and write
the result to one or more destinations.

```toml
[[template]]
src = "templates/foo.conf.tmpl"
dst = ".config/foo/foo.conf"

[[template]]
condition = "os == 'linux'"
src = "templates/settings.json.tmpl"
dst = [".config/Code/User/settings.json", ".config/VSCodium/User/settings.json"]
```

`dst` is a single path or a list of paths, relative to the destination path.

Templates have the same variables and functions as
[conditions](#conditions): variables as fields (`{{.os}}`, `{{.hostname}}`)
and functions as template functions (`{{env "HOME"}}`,
`{{lookPath "pinentry-mac" "pinentry-gnome3"}}`,
`{{if isDir "/Applications/Emacs.app"}}...{{end}}`). In addition,
`{{expr "..."}}` returns the value of an expression evaluated like a
condition, e.g. ``{{if expr `hostname in ["yak", "flam"]`}}...{{end}}``.
Using a field that does not exist, such as `{{.hostnme}}`, is an error.

If a `dst` ends in `.json` or `.toml`, the rendered output must parse as that
format, or the file is not written.

A destination is only written when its content changes, and is then replaced
atomically. It gets the permissions of the template file, or those set with
`mode`, an octal string as for chmod:

```toml
[[template]]
src = "templates/secret.conf.tmpl"
dst = ".config/foo/secret.conf"
mode = "0600"
```

If only the permissions differ, they are changed without rewriting the file.

`existing` decides what happens when a destination already exists with
different content:

- `overwrite` (default): replace it.
- `skip`: leave an existing destination alone, permissions included. Only a
  missing destination is written, so the template is a starting point that can
  be edited afterwards.
- `error`: fail, unless `dotfiles` runs with `-f`. A destination with the same
  content is fine. Changing the template also counts as different content,
  because `dotfiles` does not keep track of what it wrote earlier.

Git only records whether a file is executable, so a template's permissions
depend on how the repository was cloned; use `mode` when they matter. A
destination that is a symlink is refused, so a template never writes into a
file linked from a repository.

The template source is an ordinary file in the repository; it is linked like
any other file unless it is ignored or mounted elsewhere.

### Conditions

The `condition` field in `[[mount]]`, `[[script]]`, `[[template]]` and
repository remotes is an [expr](https://github.com/expr-lang/expr) expression
that decides whether the action is performed, e.g.
`os == 'linux' && hostname in ['yak', 'flam']`.

Variables:

- `os`: the operating system (Go's `GOOS`, e.g. `linux` or `darwin`).
- `arch`: the CPU architecture (Go's `GOARCH`, e.g. `amd64` or `arm64`).
- `hostname`: the system hostname.

Functions:

- `env(name)`: the value of an environment variable.
- `lookPath(name, ...)`: the full path of the first of the named programs found
  in `PATH`, or an empty string if none is found. A name containing a `/` is
  checked as a path instead of being looked up in `PATH`.
- `exists(path)`, `isFile(path)`, `isDir(path)`: whether a path exists, is a
  regular file or is a directory, following symlinks.

Relative paths are relative to the destination path (the home directory
unless `-dst` is given), never to the directory `dotfiles` runs in. `~` is not
expanded.

Not finding a program or path is not an error, so it can be tested for, e.g.
`condition = "lookPath('alacritty') != ''"`.

### Templated Paths

`src` and `dst` in `[[mount]]`, `[[go]]` and `[[template]]` are templates,
with the same variables and functions as [templates](#templates):

```toml
[[mount]]
src = 'config/foo-{{.os}}.conf'
dst = '.config/foo/foo.conf'
```

They are rendered when the config is loaded. An entry whose `src` or `dst`
renders to an empty string is skipped, e.g.
`dst = '{{if eq .os "linux"}}.config/foo{{end}}'`; for `[[template]]`, only
that `dst` is skipped. `[[script]]` `src` is shell code and is not a template.

### Default Root Item Handling

Files and directories at the root of the repository that are not ignored or
explicitly mounted are automatically symlinked to the destination path. If a
file name does not start with a dot, one is prepended (e.g., `bashrc` becomes
`~/.bashrc`).

## Order of Execution

`install` first plans every action of every repository, then runs them. `plan`
prints the same list, in the same order, without running anything.

### Planning

Repositories are taken in alphabetical order of their directory names in the
repositories path, or in the order given with `-repository`. For each
repository, the config is loaded, `src` and `dst` paths are rendered, and
`condition`s are evaluated.

All of this happens before anything runs. Conditions and templated paths
therefore see the state from before the run: `isDir(".config/foo")` is false
even if a mount earlier in the same run creates `.config/foo`. The content of
a `[[template]]` file is rendered later, when its action runs.

A repository whose config fails to load or to plan is skipped completely,
including its scripts.

### Running

The actions run in four stages. Each stage goes through all repositories in
order before the next stage starts:

1. **go install**: `[[go]]` entries in config order, then the directories in
   `cmd/` that no `[[go]]` entry names, alphabetically.
2. **pre scripts**: `[[script]]` entries with `phase = "pre"`, in config order.
3. **Files**, for one repository at a time:
   1. `[[mount]]` entries in config order; the matches of a wildcard `src` in
      alphabetical order.
   2. Root items that are not ignored or mounted, alphabetically.
   3. `[git]` settings, in alphabetical order of their keys.
   4. `[[template]]` entries in config order, each `dst` in order.
   5. Default-phase `[[script]]` entries in config order.
4. **post scripts**: `[[script]]` entries with `phase = "post"`, in config
   order.

For example, with the repositories `base` and `private`, a mount in `private`
runs after all of `base`'s files, git settings, templates and default scripts,
and a post script in `base` runs after everything in `private` except
`private`'s own post scripts.

A failing action does not stop the run. All errors are listed at the end, and
`dotfiles` exits with a non-zero status.

