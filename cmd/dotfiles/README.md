# dotfiles-go

A tool to manage and synchronize dotfiles from multiple repositories. It
automates the process of creating symlinks, configuring git, and running setup
scripts.

`dotfiles doc` prints this file. `dotfiles doc toc` prints its numbered table
of contents, and `dotfiles doc <topic>` prints only the sections whose number
or heading matches the topic, e.g. `dotfiles doc 2.7`, `dotfiles doc starlark`
or `dotfiles doc order of execution`; an exact heading wins over headings that
only contain the topic. Pipe it to a pager, e.g. `dotfiles doc | less`.

After this file, `dotfiles doc` prints appendices that it generates from the
program, so that they always match it: A, the files that are always ignored;
B, every key of `dotfiles.toml`; C, the options with their environment
variables; and D, every variable and function of expressions, templates and
Starlark scripts. `dotfiles doc appendix b` prints only appendix B.

## Dotfiles Repositories

`dotfiles` works on a set of dotfiles repositories: it finds them on disk
and, with `[repositories]`, knows which ones should exist and where their
remotes are.

### Discovery

By default, the tool looks for repositories in `~/src/dotfiles`. Each directory
in this path is treated as a separate dotfiles repository. This path can be
changed using the `-repos` flag or the `DOTFILES_REPOS` environment variable.

### `[repositories]`

Known dotfiles repositories and the git remotes each of them should
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

### Pulling and Pushing

`pull`, `push` and `publish` work on the main branch (`main`, or else
`master`) of every repository, with the repositories in parallel:

- `dotfiles pull [-no-install]`: pull from the upstream remote, then run
  `install`. `install` is skipped with `-no-install`, or if a pull failed. A
  repository that has another branch checked out is skipped, since pulling
  would merge or rebase that branch.
- `dotfiles push`: push to the private and backup remotes. One repository's
  remotes are pushed one after the other.
- `dotfiles publish`: push public repositories to their publish remotes.

`dotfiles repos clone` clones in parallel too, with the same host check and
summary.

First, each ssh host that the remotes point to is checked once, with `git
ls-remote`. If a host does not answer within 20 seconds or the login fails,
its remotes are skipped, so a broken login is reported once rather than once
per repository. With ssh's `ControlMaster auto` and `ControlPersist`, the
check's connection becomes the master that the pulls and pushes share.

At the end, a summary shows one line per repository and remote: the number
of commits pulled or pushed and their `git diff --shortstat`, `up to date`,
`new branch`, `forced update`, `cloned`, `SKIPPED` with the reason, or
`FAILED` with git's error. A pull counts the commits that came from the
remote, not local commits that it rebased, and also shows how many local
commits are not pushed. On a terminal the summary is colored. Pass `-v` to
also print each git command as it starts.

Interrupting with Ctrl-C (or SIGTERM) stops the running git commands, lets
them clean up, and still prints the summary, which shows for each repository
the git command it was interrupted in, or that it was not started. `install`
is then skipped. A second Ctrl-C quits at once.

## Order of Execution

`install` first plans every action of every repository, then runs them. `plan`
prints the same list, in the same order, without running anything.

### Planning

Repositories are taken in alphabetical order of their directory names in the
repositories path, or in the order given with `-repository`. Planning has
five steps, each done for all repositories before the next:

1. **Read**: the config of every repository is read and checked, and the code
   of `[[starlark]]` entries is compiled.
2. **Select the theme**: the themes in the `[theme]` `dirs` of all
   repositories are loaded, and the theme variant in use is chosen (see
   `[theme]`).
3. **Merge variables**: the variables of all configs are merged, giving each
   repository its own view of them (see Precedence).
4. **Resolve**: each repository's variables are resolved, rendering them as
   templates, and then its templated `src` and `dst` paths, `[[script]]`
   sources and `[git]` values are rendered with them.
5. **Plan**: each repository's `condition`s are evaluated and its actions are
   planned.

The variables of all repositories are known before any of them is resolved,
so the order of repositories does not matter for that; which definition a
repository gets is decided by `[repositories]` `names`.

All of this happens before anything runs. Variables, conditions and templated
paths therefore see the state from before the run: `isDir(".config/foo")` is
false even if a mount earlier in the same run creates `.config/foo`. The
content of a `[[template]]` file is rendered later, when its action runs, and
`[[starlark]]` scripts run then too, both with the variables resolved during
planning; their `exists`, `isFile`, `isDir` and `lookPath` see the files as
they are at that point in the run.

A repository whose config fails to load, whose variables fail to resolve, or
that fails to plan is skipped completely, including its scripts. Its
variables are still available to the other repositories if its config could
be read.

### Running

The actions run in four stages, with the variables resolved during planning.
Each stage goes through all repositories in order before the next stage
starts:

1. **go install**: `[[go]]` entries in config order, then the directories in
   `cmd/` that no `[[go]]` entry names, alphabetically.
2. **pre scripts**: `[[script]]` entries with `phase = "pre"`, in config order.
3. **Files**, for one repository at a time:
   1. `[[mount]]` entries in config order; the matches of a wildcard `src` in
      alphabetical order.
   2. Root items that are not ignored or mounted, alphabetically.
   3. `[git]` settings, in alphabetical order of their keys.
   4. `[[template]]` entries in config order, each `dst` in order. The
      template file is rendered now, with the variables.
   5. `[[starlark]]` entries in config order, each `dst` in order; the
      matches of a wildcard `dst` in alphabetical order. The script gets the
      variables as `vars`.
   6. Default-phase `[[script]]` entries in config order.
4. **post scripts**: `[[script]]` entries with `phase = "post"`, in config
   order.

For example, with the repositories `base` and `private`, a mount in `private`
runs after all of `base`'s files, git settings, templates, `[[starlark]]`
entries and default scripts, and a post script in `base` runs after everything
in `private` except `private`'s own post scripts.

A failing action does not stop the run. All errors are listed at the end, and
`dotfiles` exits with a non-zero status.

### Installing Part of the Dotfiles

`install` and `plan` take arguments that limit them to some of the actions,
for example after editing one template:

```sh
dotfiles install .                # what is in the current directory
dotfiles install base private     # the repositories base and private
dotfiles install ./mpdconf.tmpl   # the actions that read this file
dotfiles install ~/.mpdconf       # the actions that write this path
```

Every repository is still loaded and planned, so variables and conditions are
the same as in a full install; only the selected actions run, in their usual
order. Unlike `-repository`, which leaves the other repositories out
completely, their `[vars]` are still used.

An argument that is `.` or `..` or contains a `/` is a path; any other
argument is the name of a repository or, if no repository has that name, a
path that exists. A repository name selects all of its actions. A path
selects:

- all actions of the repositories inside it, such as `.` at the top of a
  repository;
- the actions whose source in a repository or destination is the path or is
  inside it: symlinks, copies, templates, `[[go]]` programs and the `dst`
  of `[[starlark]]`;
- the actions whose source or destination contains the path, so a file in a
  directory that is symlinked as a whole selects that symlink.

`[[script]]` entries, `[git]` settings and the code of `[[starlark]]` entries
are not files, so a path selects them only together with their whole
repository. An argument that selects nothing is an error.

## dotfiles.toml Configuration

Each repository can contain a `dotfiles.toml` or `.dotfiles.toml` file to
configure its behavior.

### General

This part covers what works the same way in every section: expressions and
conditions, templates, templated paths, and the files that are linked without
any section.

After it, each section of the file is described in the order in which `install`
uses it (see Order of Execution): `[theme]` and `[vars]` first, because the
theme is chosen and variables are resolved before anything else, then the
entries from `[[go]]` to `[[script]]`.
`[repositories]`, which only the `repos` commands use, is described under
`[repositories]`.

#### Expressions and Conditions

Expressions are written in [expr](https://github.com/expr-lang/expr) and
can be used almost anywhere: in `condition` fields, in templates with
`{{expr "..."}}`, and so in `[vars]` values, paths, `[git]` values and
`[[script]]` code.

The `condition` field in `[[mount]]`, `[[script]]`, `[[template]]`,
`[[starlark]]` and repository remotes is an expression that decides whether
the action is performed, e.g. `os == 'linux' && hostname in ['yak', 'flam']`.

The same variables and functions are available in conditions, templates and
Starlark scripts, written as each expects:

| Variable   | Value                                                  | Condition    | Template          | Starlark        |
|------------|--------------------------------------------------------|--------------|-------------------|-----------------|
| `os`       | The operating system (Go's `GOOS`: `linux`, `darwin`)  | `os`         | `{{.os}}`         | `os`            |
| `arch`     | The CPU architecture (Go's `GOARCH`: `amd64`, `arm64`) | `arch`       | `{{.arch}}`       | `arch`          |
| `hostname` | The system hostname                                    | `hostname`   | `{{.hostname}}`   | `hostname`      |
| `vars`     | The variables, all strings                             | `vars.email` | `{{.vars.email}}` | `vars["email"]` |
| `theme`    | The theme variant in use (see `[theme]`)               | `theme.mode` | `{{.theme.mode}}` | `theme["mode"]` |

| Function              | Returns                                                                                                                                                                                           | Condition                                       | Template                                           |
|-----------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------|----------------------------------------------------|
| `env(name)`           | The value of an environment variable, or `""`.                                                                                                                                                    | `env('EDITOR')`                                 | `{{env "EDITOR"}}`                                 |
| `lookPath(name, ...)` | The full path of the first of the named programs found in `PATH`, or `""`. A name containing a `/` is checked as a path instead.                                                                  | `lookPath('nvim', 'vim')`                       | `{{lookPath "nvim" "vim"}}`                        |
| `exists(path)`        | Whether `path` exists, following symlinks.                                                                                                                                                        | `exists('.ssh')`                                | `{{exists ".ssh"}}`                                |
| `isFile(path)`        | Whether `path` is a regular file, following symlinks.                                                                                                                                             | `isFile('.zshrc')`                              | `{{isFile ".zshrc"}}`                              |
| `isDir(path)`         | Whether `path` is a directory, following symlinks.                                                                                                                                                | `isDir('.config/foo')`                          | `{{isDir ".config/foo"}}`                          |
| `parseBool(s)`        | `s` as a bool; `true`, `false`, `1`, `0`, `t` and `f` are accepted, anything else is an error.                                                                                                    | `parseBool(vars.gui)`                           | `{{parseBool .vars.gui}}`                          |
| `hexDigits(color)`    | A `#rrggbb` color as `rrggbb`. Anything but a `#rrggbb` color is an error, here and in `rgb` and `hex0x`.                                                                                         | `hexDigits(theme.colors.red)`                   | `{{hexDigits .theme.colors.red}}`                  |
| `rgb(color)`          | A `#rrggbb` color as `r,g,b` in decimal, e.g. `16,60,72`.                                                                                                                                         | `rgb(theme.colors.bg_0)`                        | `{{rgb .theme.colors.bg_0}}`                       |
| `hex0x(color)`        | A `#rrggbb` color as `0xrrggbb`.                                                                                                                                                                  | `hex0x(theme.colors.bg_0)`                      | `{{hex0x .theme.colors.bg_0}}`                     |
| `mix(a, b, t)`        | The `#rrggbb` color `t` of the way from `a` to `b`, `t` between 0 and 1, mixed in OKLab so that equal steps look equal: `mix(theme.colors.bg_0, theme.colors.green, 0.1)` is `bg_0` tinted green. | `mix(theme.colors.bg_0, theme.colors.red, 0.1)` | `{{mix .theme.colors.bg_0 .theme.colors.red 0.1}}` |

Starlark scripts call the functions as conditions do, with single or double
quotes: `lookPath("nvim", "vim")`. `mix` takes its third argument as a number, `0.1`, in all three.

Relative paths are relative to the destination path (the home directory
unless `-dst` is given), never to the directory `dotfiles` runs in. `~` is not
expanded.

Not finding a program or path is not an error, so it can be tested for, e.g.
`condition = "lookPath('alacritty') != ''"`.

#### Useful expr Features

[expr](https://expr-lang.org/docs/language-definition) has many operators
and built-in functions; these come up most in conditions and in
`{{expr "..."}}`:

| Expression                                                | Use                                                       |
|-----------------------------------------------------------|-----------------------------------------------------------|
| `hostname in ['yak', 'flam']`, `os not in [...]`          | Test membership in a list.                                |
| `&&`, `\|\|`, `!` or `and`, `or`, `not`                   | Combine tests.                                            |
| `hostname startsWith 'work-'`, `endsWith`, `contains`     | Test parts of a string.                                   |
| `hostname matches '^(yak\|flam)$'`                        | Test a string against a regular expression.               |
| `vars.theme ?? 'dark'`                                    | A default for a variable that may not be defined.         |
| `os == 'darwin' ? '13' : '11'`                            | Choose between two values.                                |
| `{'yak': '165', 'flam': '170'}[hostname] ?? '96'`         | Look a value up per host, with a default.                 |
| `let size = int(vars.font_size); size > 10`               | Name a value to use it more than once.                    |
| `int(s)`, `float(s)`, `string(x)`                         | Convert values, e.g. vars, which are strings.             |
| `lower(s)`, `upper(s)`, `trim(s)`, `replace(s, old, new)` | Change strings.                                           |
| `split(s, ',')`, `join(xs, ',')`, `len(x)`                | Split and join strings; sizes of strings, lists and maps. |
| `any(xs, {lookPath(#) != ''})`, `all(...)`                | Test every element of a list; `#` is the element.         |
| `filter(xs, {...})`, `map(xs, {...})`                     | Select or change the elements of a list.                  |
| `1..3`                                                    | A list of integers, `[1, 2, 3]`.                          |

In a TOML value written in double quotes, write string literals with single
quotes, as above.

#### Templates

Many values in `dotfiles.toml` are Go
[text/template](https://pkg.go.dev/text/template) templates: `[vars]` values,
paths, `[git]` values, `[[script]]` code, and the files of `[[template]]`
entries. A value without `{{` is used as it is.

Templates have the same variables and functions as expressions: variables as
fields (`{{.os}}`, `{{.hostname}}`, `{{.vars.email}}`) and functions as
template functions (`{{env "HOME"}}`,
`{{lookPath "pinentry-mac" "pinentry-gnome3"}}`,
`{{if isDir "/Applications/Emacs.app"}}...{{end}}`). In addition:

- `{{expr "..."}}` returns the value of an expression evaluated like a
  condition, e.g. ``{{if expr `hostname in ["yak", "flam"]`}}...{{end}}``.
- `{{shellQuote s}}` quotes `s` as a single word for a POSIX shell (`sh`,
  `bash`, `zsh`): `two words` becomes `'two words'`, and a word that needs no
  quoting is left as it is. Use it for values in `[[script]]` code and in
  templates of shell files.

Using a field that does not exist, such as `{{.hostnme}}`, is an error.

#### Templated Paths

`src` and `dst` in `[[mount]]`, `[[go]]` and `[[template]]`, and `dst` in
`[[starlark]]`, are templates, with the same variables and functions as
templates:

```toml
[[mount]]
src = 'config/foo-{{.os}}.conf'
dst = '.config/foo/foo.conf'
```

They are rendered when the config is loaded. An entry whose `src` or `dst`
renders to an empty string is skipped, e.g.
`dst = '{{if eq .os "linux"}}.config/foo{{end}}'`; for `[[template]]`, only
that `dst` is skipped. `[[script]]` `src` is shell code but a template too (see
`[[script]]`); `[[starlark]]` `src` is not a template, because the script gets
the variables directly.

#### Default Root Item Handling

Files and directories at the root of the repository that are not ignored or
explicitly mounted are automatically symlinked to the destination path. If a
file name does not start with a dot, one is prepended (e.g., `bashrc` becomes
`~/.bashrc`).

### `[theme]`

A theme is a set of color palettes, one for each of its variants, such as
the dark and the light variant of Selenized. `[theme]` says where themes are
and which variants dark and light mode use; the variant in use is available
as `theme` in conditions, templates and Starlark scripts.

```toml
[theme]
dirs  = ["config-base/themes"]
dark  = "selenized:dark"
light = "selenized:light"
mode  = "light"
```

| Key     | Meaning                                                                                    |
|---------|--------------------------------------------------------------------------------------------|
| `dirs`  | Directories, relative to the repository, whose subdirectories are themes.                  |
| `dark`  | The variant of dark mode, as `theme:variant`. It must be a variant with `mode = "dark"`.   |
| `light` | The variant of light mode, as `theme:variant`. It must be a variant with `mode = "light"`. |
| `mode`  | The mode until one is chosen: `dark` or `light` (the default).                             |

The `dirs` of all repositories are used. A theme name in two of them is an
error, and a theme that fails to load is reported and left out. Unlike
`[vars]`, `dark`, `light` and `mode` are the same for every repository: each
comes from the first repository in repository order (see Precedence) that
sets it, with a warning if others set it differently.

#### Theme Directories

A theme is a directory; its name is the theme name:

```
selenized/
├── theme.toml          metadata (creator, url, license, description) and [ansi]
└── variants/
    ├── dark.toml       mode, [colors] and [apps] of the variant dark
    └── light.toml
```

A variant file has its mode, its colors and, optionally, settings for apps,
such as the name of an editor theme:

```toml
mode = "dark"

[colors]
bg_0  = "#103c48"
bg_1  = "#184956"
# ...
br_red = "#ff665c"

[apps]
emacs = "my-solarized-selenized-dark"
```

Every variant must have the colors `bg_0`, `bg_1`, `bg_2` (backgrounds),
`dim_0` (dim text), `fg_0`, `fg_1` (text) and the accents `yellow`, `orange`,
`red`, `magenta`, `violet`, `blue`, `cyan` and `green`. A higher number is
further from `bg_0`. A theme can add other colors, such as `br_red`, or
`dim_1`, dim text that reads better than `dim_0` on `bg_1`; every
variant of a theme must have the same colors and the same apps. Colors are
written `#rrggbb`; names are lower case letters, digits and `_`.

#### Background Images

`apps.background` is the desktop background of a variant: an image file,
relative to the theme directory or absolute, or, after a `:`, an
[expression](#expressions-and-conditions) that generates an image from the colors of the
variant:

```toml
[apps]
background      = ":stripes()"
background_mode = "tile"
```

In the expression, the colors of the variant are variables (`bg_0`, `blue`,
`color4`), and it has `mix` and the generators below. A color can also be
written as a string, `"#184956"`, or mixed, `mix(bg_0, blue, 0.1)`:
`:stripes([bg_0, mix(bg_0, blue, 0.15)])` gives stripes tinted blue.

`dotfiles theme background-image` prints the absolute path of the background
image, a tile of `bg_0` if the variant has none. A generated one is drawn
anew on every call, at the `dpi` var, or at 160 DPI without one, and written uncompressed
to `$XDG_RUNTIME_DIR/dotfiles/background.png`, replacing the last one. A
`[[script]]` with `on = "theme"` can set the desktop background with it:

```sh
hsetroot -tile "$(dotfiles theme background-image)"
```

Both generators draw soft stripes: across them, the color goes smoothly
through `colors`, at least two (default `[bg_0, bg_1]`), and back again.
Distances are pixels at 96 DPI.

| Generator               | Image                                                                                                                                                                                                                                   |
|-------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `diagonal(colors)`      | The Selenized backgrounds: stripes from the top left to the bottom right. Always the same.                                                                                                                                              |
| `stripes(colors)`       | Diagonal stripes from the top left or the top right, tilted by up to 10 degrees. They change with a seed in the [theme state](#the-theme-in-use) that `dotfiles theme set`, `toggle` and `apply` replace, so other runs of dotfiles draw the same background and every switch draws a new one. |
| `chevron(colors)`       | Classic continuous 45° zig-zag V-bands with equal width and 50/50 color balance.                                                                                                                                                        |
| `herringbone(colors)`   | Classic staggered parquet tweed weave of alternating 45° diagonal blocks.                                                                                                                                                               |
| `houndstooth(colors)`   | The iconic 4-pointed menswear broken check from traditional 2/2 twill weave.                                                                                                                                                            |
| `basketweave(colors)`   | Alternating square blocks of horizontal and vertical bars (Oxford cloth / Panama weave).                                                                                                                                                |
| `diamond(colors)`       | 45° diamond lattice check of equal-area lozenges (harlequin masonry).                                                                                                                                                                   |
| `meander(colors)`       | Classical interlocking Greek key (meander) architectural fretwork.                                                                                                                                                                      |
| `crosses(colors)`       | Byzantine Greek cross mosaic tessellation of interlocking plus signs.                                                                                                                                                                   |
| `parquet(colors)`       | Interlocking European windmill parquet wood-block floor pattern.                                                                                                                                                                        |
| `tatami(colors)`        | Traditional Japanese 8-mat 2:1 tatami arrangement.                                                                                                                                                                                      |
| `random(patterns)`      | Selects randomly among background generators (or patterns passed as arguments).                                                                                                                                                         |

`dotfiles theme lint` reports an expression that fails or does not return a
background, and a missing image file.

The accent names indicate positions around the color wheel rather than strict
literal color definitions: they provide an evenly spaced 8-accent spectrum
(spaced at ~45° intervals: red, orange, yellow, green, cyan, blue, violet,
magenta) so that templates and syntax highlighters can rely on visually distinct
hues across all themes. In particular, `magenta` and `violet` represent the two
halves of the purple spectrum: `magenta` is the warmer, reddish-purple tone
(~315°), while `violet` is the cooler, bluish-purple tone (~270°). When adapting
a palette that only defines a single purple upstream, `magenta` is assigned to
that purple, and `violet` is derived by shifting it toward blue (e.g. in OKLCH
hue 300) to ensure both remain distinct.

`theme.toml` contains metadata for the theme (optional `creator`, `url`,
`license`, and `description` fields to give credit to the upstream palette) and
maps the 16 terminal colors, `color0` to `color15`, to colors of the variants:

```toml
creator = "Jan Warchoł"
url = "https://github.com/jan-warchol/selenized"
license = "MIT"
description = """
The terminal colors, as Selenized maps them."""

[ansi]
color0 = "bg_1"
color1 = "red"
# ...
color15 = "fg_1"
```

#### The Theme in Use

The variant in use is chosen by the first of:

1. the `-theme` option (`DOTFILES_THEME`): `theme:variant`, or `:variant` for
   another variant of the theme that would be chosen otherwise, e.g.
   `dotfiles -theme :dark plan`;
2. `theme` in the state file `~/.local/state/dotfiles/theme.toml`, which
   `dotfiles theme` writes;
3. `[theme]` `dark` or `light`, for the mode of the state file, else
   `[theme]` `mode`, else light.

Without `[theme]` `dark` and `light` and without the option, there is no
theme, and using `theme` is an error.

`theme` has these keys:

| Key               | Value                                                                             |
|-------------------|-----------------------------------------------------------------------------------|
| `name`            | The theme, e.g. `selenized`.                                                      |
| `variant`         | The variant, e.g. `dark`.                                                         |
| `mode`            | `dark` or `light`.                                                                |
| `dir`             | The absolute path of the theme directory.                                         |
| `colors`          | The colors of the variant, `br_*` fallbacks, and `color0` to `color15`.           |
| `terminal`        | The terminal colors: `normal` and `bright` (also `bright_ansi`, `bright_accents`). |
| `terminal_bright` | `palette` (default) or `accents`.                                                 |
| `apps`            | The `[apps]` of the variant.                                                      |

`[theme]` `terminal_bright` controls how terminal bright colors are mapped:

- `palette` (the default): ANSI `color8` through `color15` and `theme.terminal.bright` use the variant's `[ansi]` mapping from `theme.toml` (e.g. Solarized maps ANSI 9 to orange and 13 to violet).
- `accents`: ANSI `color8` through `color15` and `theme.terminal.bright` map to `br_black`, `br_red`, ..., `br_white`. If a palette does not define a bright variant for an accent, it automatically falls back to the base accent (`br_black` falls back to `bg_2`, and `br_white` to `fg_1`).

`theme.terminal` provides `normal` and `bright` maps with keys `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, and `white`, ready for terminal configurations such as Alacritty.

```toml
[vars]
term_bg = '{{.theme.colors.bg_0}}'

[[mount]]
condition = "theme.mode == 'dark'"
src = "config/dark-only.conf"
dst = ".config/foo/extra.conf"
```

Using a color or key that does not exist is an error. To make one optional,
give a default: `theme.colors.dim_1 ?? theme.colors.dim_0` in an expression,
`{{or (index .theme.colors "dim_1") .theme.colors.dim_0}}` in a template. A
template can test for a theme with `{{if .theme}}`.

In `[[starlark]]` code, `read` also reads files in the theme directory:
`read(theme["dir"] + "/extra.toml")`.

#### Switching the Theme

`dotfiles theme` shows and switches the theme:

| Command                          | What it does                                                                                                                                                                                                                             |
|----------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `dotfiles theme`                 | Shows the theme variant in use and its mode.                                                                                                                                                                                             |
| `dotfiles theme list`            | Lists every variant of every theme, marking the one in use with `*`.                                                                                                                                                                     |
| `dotfiles theme preview`         | Shows the colors of the variant in use in a terminal with 24-bit colors: swatches, text on each background with its contrast ratio, surface hierarchy, accent spectrum, terminal colors, styled terminal text, warnings for invisible text, and app metadata. `preview dark`, `preview selenized:light` and `preview :black` show another variant, and `preview -all` shows all variants. |
| `dotfiles theme lint`            | Lints a theme variant or all themes (`-all`) for contrast and styling issues (such as invisible text combinations). Defaults to the variant in use; accepts `<variant>` or `-theme`. |
| `dotfiles theme color <name>`    | Prints a single named color in hex format (or `-format raw`, `0x`, `rgb`). `-theme` specifies another variant.                                                                                                                           |
| `dotfiles theme set dark`        | Switches to dark mode, with `[theme]` `dark`; `set light` likewise.                                                                                                                                                                      |
| `dotfiles theme set selenized:x` | Switches to a variant, and to its mode; `set :x` keeps the theme in use.                                                                                                                                                                 |
| `dotfiles theme toggle`          | Switches between dark and light mode, with `[theme]` `dark` and `light`.                                                                                                                                                                 |
| `dotfiles theme apply`           | Applies the theme variant in use: installs files and runs `[[script]]` entries with `on = "theme"`, with new variations of generated backgrounds.                                                                                                       |
| `dotfiles theme background-image`| Writes the background image of the theme variant in use and prints its path (see [Background Images](#background-images)).                                                                                                                              |

A switch writes the state file, then installs the files of every repository
as `dotfiles install` does, with the new theme, and runs the `[[script]]`
entries with `on = "theme"` instead of the usual ones. `[[go]]` packages are
not installed. Files whose content does not change are not written, so only
what depends on the theme changes.

### `[vars]` and `[default_vars]`

`[vars]` defines variables for templates, templated paths, scripts, `[git]`
values, conditions and `[[starlark]]` entries:

```toml
[vars]
email     = 'me@example.com'
font_size = '{{if eq .hostname "laptop"}}13{{else}}11{{end}}'
editor    = '{{lookPath "nvim" "vim" "vi"}}'
big_font  = '{{expr `int(vars.font_size) + 2`}}'
```

They are used as `{{.vars.email}}` in templates and `vars.email` in
expressions.

Every value is a string, rendered as a template when the config is loaded, so
`font_size = 11` is an error; write `font_size = '11'`. A value can refer to
other variables as `.vars.x`, `$.vars.x`, `index .vars "x"`, or `vars.x` inside
`expr`; a reference cycle is an error.

Because every variable is a string, convert it where another type is needed:
`int(vars.font_size) > 10` or `parseBool(vars.gui)` in a condition,
`{{if parseBool .vars.gui}}` in a template. Note that `{{if .vars.gui}}` is
true even for `'false'`, because the string is not empty.

Using a variable that is not defined is an error, in templates and in
expressions. To make one optional, give a default: `vars.font_size ?? '11'` in
an expression, `{{or (index .vars "font_size") "11"}}` in a template.

`[default_vars]` defines fallback values that only the repository itself sees,
used only if no repository sets the variable in `[vars]`:

```toml
[default_vars]
font_size = '11'
```

A variable can not be in both `[vars]` and `[default_vars]` of the same
repository.

#### Precedence

Variables in `[vars]` are shared between repositories; `[default_vars]` are
not. For each variable, a repository uses the first of:

1. its own `[vars]`;
2. the `[vars]` of the other repositories, the first in repository order;
3. its own `[default_vars]`;
4. nothing: using the variable is an error, unless it has a default where it
   is used (`vars.x ?? 'd'`, see above).

Repository order is the order of `[repositories]` `names` (merged from all
repositories, first occurrence first), then the repositories not named there,
by path. It is not the order in which repositories are planned.

For example, with `names = ["base", "work", "games"]`:

```toml
# base/dotfiles.toml
[vars]
font_size = '11'
editor    = 'vim'
```

```toml
# work/dotfiles.toml
[vars]
font_size = '13'
```

```toml
# games/dotfiles.toml
[default_vars]
font_size = '9'
pager     = 'less'
```

| Variable    | base          | work          | games                                |
|-------------|---------------|---------------|--------------------------------------|
| `font_size` | `11` (1)      | `13` (1)      | `11` (2: `base` comes before `work`) |
| `editor`    | `vim` (1)     | `vim` (2)     | `vim` (2)                            |
| `pager`     | undefined (4) | undefined (4) | `less` (3)                           |

`games` gets `11` rather than its own default `9`, because a `[vars]`
definition in any repository comes before `[default_vars]`. Because `base` and
`work` define `font_size` differently and `games` does not define it in
`[vars]`, `dotfiles` prints a warning naming the definition `games` uses.
Nothing is printed for differences that only involve each repository's own
`[vars]` or `[default_vars]`.

### `[[go]]`

Install Go programs from the repository with `go install`, run in the
package's directory:

```toml
[[go]]
src = "cmd/batt"
condition = "hostname in ['yak']"
```

`src` is a directory in the repository. Every directory in `cmd/` that no
`[[go]]` entry names is installed too, so a `[[go]]` entry is only needed for
a program elsewhere or for a `condition`. Programs are installed before
anything else runs, so scripts can use them.

### `[[ignore]]`

Specify patterns for files and directories that should not be synchronized.

```toml
[[ignore]]
match = ["*.log", "temp", "local-config"]

[[ignore]]
condition = "os == 'darwin'"
match = ["config/systemd"]
```

A pattern is a `filepath.Match` pattern that is matched against both the
base name of a path and its path relative to the repository root, so
`context.md` ignores every file with that name and `config/systemd` only that
directory. Leading and trailing slashes never match.

### `[[mount]]`

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

### `[git]`

Set global git configuration values when the repository is synchronized.

```toml
[git]
"user.name" = "Your Name"
"user.email" = "user@example.com"
```

To unset a git configuration value, set it to an empty string.

Values are templates, rendered when the config is loaded, so they can come from
variables or differ between machines. A value that renders to an empty string
unsets the key:

```toml
[git]
"user.email" = "{{.vars.email}}"
"core.editor" = '{{lookPath "nvim" "vim" "vi"}}'
"gpg.program" = '{{if eq .os "darwin"}}/opt/homebrew/bin/gpg{{end}}'
```

### `[[template]]`

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
The file is a template, rendered when the action runs.

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

### `[[starlark]]`

Edit structured config files, including ones that an application also
writes to, with a [Starlark](https://github.com/bazelbuild/starlark/blob/master/spec.md)
script.

```toml
[vars]
dpi = '{{if eq .hostname "laptop"}}144{{else}}96{{end}}'

[[starlark]]
dst = '.config/foo/display.json'
src = '''
dpi = int(vars["dpi"])

def scaled(px):
    """Returns px, a size in pixels at 96 DPI, scaled to this display."""
    return px * dpi // 96

merge(doc, {
    "display": {"scale": dpi / 96, "cursor_size": scaled(24)},
    "panel": {"height": scaled(28), "icon_size": scaled(16)},
    "legacy_dpi": None,
})
'''
```

`src` is the script itself, like the shell code of `[[script]]`; write it as
a TOML multi-line literal string (`'''...'''`). Each `dst` is read and
decoded into the dict `doc`, the script changes it, and the result is written
back in the same format.

| Field       | Default            | Meaning                                                                                                               |
|-------------|--------------------|-----------------------------------------------------------------------------------------------------------------------|
| `src`       | required           | The Starlark script.                                                                                                  |
| `dst`       | required           | A path or a list of paths, relative to the destination path. Wildcards (`*/settings.json`) only match existing files. |
| `condition` |                    | A condition.                                                                                                          |
| `missing`   | `skip`             | What to do when a `dst` does not exist; see below.                                                                    |
| `format`    | from the extension | `json` or `toml`, for a `dst` whose extension is neither `.json` nor `.toml`.                                         |
| `indent`    | `2`                | Spaces per level of JSON output.                                                                                      |
| `mode`      | `0644`             | Permissions of a file created with `missing = "create"`, an octal string as for `[[template]]`.                       |

| `missing` | When a `dst` does not exist                  |
|-----------|----------------------------------------------|
| `skip`    | Do nothing.                                  |
| `create`  | Start from an empty dict and write the file. |
| `error`   | Fail.                                        |

A file is only rewritten when a value changes: a file that differs only in
key order, whitespace or comments is left alone. When it is rewritten, keys
are sorted and comments are lost. An existing file keeps its permissions, and
a `dst` that is a symlink is refused.

#### Names in Scripts

Scripts have the variables and functions of expressions, and these:

| Name                      | Meaning                                                                                                                                                                                                                                    |
|---------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `doc`                     | The decoded file, a dict. Change it in place, or assign a new dict to it.                                                                                                                                                                  |
| `dst`                     | The path being edited, relative to the destination path.                                                                                                                                                                                   |
| `merge(target, patch)`    | Merges the dict `patch` into the dict `target`, as a JSON Merge Patch ([RFC 7386](https://www.rfc-editor.org/rfc/rfc7386)): dicts are merged recursively, `None` deletes a key, and anything else, lists included, replaces the old value. |
| `read(path)`              | A file in the repository, decoded by its extension (`.json` or `.toml`). A missing file is an error.                                                                                                                                       |
| `read(path, default = v)` | The same, but `v` if the file does not exist.                                                                                                                                                                                              |
| `fail(msg)`               | Fails the entry for this file; nothing is written.                                                                                                                                                                                         |
| `print(...)`              | Writes to standard error.                                                                                                                                                                                                                  |

`vars` is a dict of strings: `vars["dpi"]` fails if the variable is not
defined, `vars.get("dpi", "96")` gives a default, and `int(...)` or
`parseBool(...)` convert the value. `theme` is a dict too, or `None` if no
theme is configured: `theme["colors"]["bg_0"]`, or
`theme["colors"].get("dim_1", theme["colors"]["dim_0"])` for an optional
color.

#### Values

| JSON                           | TOML          | Starlark | Notes                                             |
|--------------------------------|---------------|----------|---------------------------------------------------|
| object                         | table         | `dict`   | Keys must be strings.                             |
| array                          | array         | `list`   | A `tuple` is written as an array too.             |
| string                         | string        | `str`    |                                                   |
| number without `.` or exponent | integer       | `int`    | TOML integers are limited to 64 bits.             |
| number with `.` or exponent    | float         | `float`  | `inf` and `nan` can't be written to JSON.         |
| `true`, `false`                | boolean       | `bool`   | Written `True` and `False` in Starlark.           |
| `null`                         |               | `None`   | Can't be written to TOML; delete the key instead. |
|                                | date and time |          | Not supported.                                    |

`1` and `1.0` are different values, so changing one into the other rewrites
the file.

#### Useful Starlark Built-ins

Starlark is a small dialect of Python; the [language
specification](https://github.com/bazelbuild/starlark/blob/master/spec.md)
describes all of it. These come up most when editing config files:

| Built-in                                                    | Use                                                                                                                       |
|-------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------|
| `d.get(k, default)`                                         | A value that may be missing.                                                                                              |
| `d.setdefault(k, default)`                                  | A value, set to `default` first if it is missing. It returns the value, so a nested dict or list can be changed in place. |
| `d.pop(k, None)`                                            | Delete a key that may be missing.                                                                                         |
| `d.update(other)`                                           | Set several keys at once; unlike `merge`, not recursively.                                                                |
| `d.clear()`                                                 | Empty a dict, e.g. `doc.clear()` to generate a whole file.                                                                |
| `d.keys()`, `d.values()`, `d.items()`                       | Loop over a dict.                                                                                                         |
| `l.append(x)`, `l.extend(xs)`, `l.remove(x)`                | Change a list in place.                                                                                                   |
| `x in l`, `k in d`                                          | Test membership, e.g. before appending.                                                                                   |
| `int(s)`, `float(s)`, `str(x)`                              | Convert values, e.g. vars, which are strings.                                                                             |
| `len(x)`, `sorted(xs)`, `range(n)`, `enumerate(xs)`         | Sizes, sorting and loops.                                                                                                 |
| `s.split()`, `s.strip()`, `s.startswith(p)`, `",".join(xs)` | Work with strings.                                                                                                        |
| `def name(args): ...`                                       | Define a function, e.g. to make the same change in several places. Recursion is not allowed.                              |

`if` and `for` work at the top level of a script, and a name can be assigned
more than once. There is no `while` and no `load()`.

#### Running Scripts Again

A script runs every time `dotfiles install` runs, on the file as it is then,
so it must leave its own result alone: setting a key, `setdefault`, `merge`
and `pop(key, None)` do, but a plain `list.append` would add the item again on
every run. Each script is therefore run a second time on its own result, and
fails without writing anything if that changes the result again.

To generate a whole file from data in the repository, clear `doc` first:

```toml
[[starlark]]
dst = '.config/alacritty/alacritty.toml'
missing = 'create'
src = '''
doc.clear()
merge(doc, read("config/alacritty/common.toml"))
merge(doc, read("config/alacritty/host." + hostname + ".toml", default = {}))
'''
```

Scripts are checked when the config is loaded, so a syntax error or a
misspelled name is reported before anything runs. Errors show the position as
`starlark[0].src:<line>:<column>`, followed by up to three lines of `src`
before it and that line with a caret under the column; an error while running
also shows the lines it was called from. Lines count from the first line of
`src`:

```
starlark[0].src:2:20: int: invalid literal with base 10: x
  1 | def scaled(px):
  2 |     return px * int(vars.get("dpi", "x")) // 96
    |                    ^
starlark[0].src:4:39: called from here
  1 | def scaled(px):
  2 |     return px * int(vars.get("dpi", "x")) // 96
  3 |
  4 | merge(doc, {"panel": {"height": scaled(28)}})
    |                                       ^
```

### `[[script]]`

Run shell scripts at different stages of the synchronization process. Scripts
are executed using an embedded shell interpreter.

- **phase = "pre"**: Runs before the files and git configuration of any
  repository are set up, after `go install`.
- **phase = "default"** (or empty): Runs after the repository's own files, git
  configuration, templates and `[[starlark]]` entries, before the next
  repository's.
- **phase = "post"**: Runs after every repository has been set up.

See Order of Execution for the full order.

`on` says when a script runs: `"install"` (the default) when `dotfiles
install` runs, `"theme"` when `dotfiles theme` switches the theme (see
Switching the Theme), or `["install", "theme"]` for both. Scripts that reload
programs after a theme switch use `on = "theme"`:

```toml
[[script]]
on  = "theme"
src = '''
gsettings set org.gnome.desktop.interface color-scheme {{shellQuote .theme.apps.color_scheme}}
xmonad --restart
'''
```

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

`src` is a template, rendered when the config is loaded, with the same
variables and functions as `[[template]]` files:

```toml
[[script]]
src = '''
foo-config set font.size "{{.vars.font_size}}"
{{if lookPath "fc-cache"}}fc-cache -f{{end}}
'''
```

The rendered text is inserted into the shell code as it is. Quote values with
`shellQuote`, which makes any string a single shell word, quotes, `$` and
newlines included: `foo-config set font.family {{shellQuote .vars.font}}`. A
script that renders to an empty string is skipped. Because `src` is rendered
during planning, functions such as `isDir` see the files as they were before
the run.
