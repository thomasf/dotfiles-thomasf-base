package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// An exitStatus is the exit status of a command that dotfiles passes on as
// its own.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// pathenvUsage is the help text of dotfiles pathenv.
const pathenvUsage = `Usage: dotfiles pathenv [-shell SHELL] [-C DIR] [-q] [-n] [reload]
       dotfiles pathenv exec [-C DIR] -- CMD [ARG...]
       dotfiles pathenv status [-C DIR] [-json]

Print the code that sets up the environment of the configuration for the
directory (default the working directory), for the pathenv function of
bash/init to evaluate. Where the configuration is not active yet (not in
PATHENV_ACTIVE), its activate entries run and it is appended to
PATHENV_ACTIVE; where it is, its refresh entries run. reload runs the
activate entries again.

exec runs CMD in the directory's environment: bash sources ~/.bashrc with
PATHENV_SKIP=1, changes to DIR, evaluates the code and runs CMD, whose exit
status is passed on; without CMD it does nothing. Where it can, dotfiles
replaces itself with that bash, which replaces itself with CMD, so CMD gets
the process of dotfiles. status shows the matching configuration and
PATHENV_ACTIVE.

The pathenv files are in the directories of [pathenv] dirs. See
dotfiles doc pathenv.

Options:
`

func (d *Dotfiles) PathenvCommand(args []string) error {
	err := d.pathenvCommand(context.Background(), args)
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		if _, ok := errors.AsType[exitStatus](err); !ok {
			fmt.Fprintf(d.Stderr, "pathenv: %v\n", err)
		}
	}
	return err
}

// pathenvOptions are the options of dotfiles pathenv and its subcommands.
type pathenvOptions struct {
	shell string
	dir   string
	quiet bool
	json  bool
}

func (d *Dotfiles) pathenvCommand(ctx context.Context, args []string) error {
	var o pathenvOptions
	fs := flag.NewFlagSet("dotfiles pathenv", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprint(d.Stderr, pathenvUsage)
		fs.PrintDefaults()
	}
	shell := os.Getenv("SHELL")
	if shell != "" {
		shell = filepath.Base(shell)
	}
	fs.StringVar(&o.shell, "shell", shell, "the shell the code is for; only bash (default the base name of $SHELL)")
	fs.StringVar(&o.dir, "C", "", "the directory (default the working directory)")
	fs.BoolVar(&o.quiet, "q", false, "say nothing where no configuration matches")
	fs.Bool("n", false, "print the code; the pathenv function prints rather than evaluates it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sub, args := "", fs.Args()
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}

	subFlags := func(name string) *flag.FlagSet {
		sfs := flag.NewFlagSet("dotfiles pathenv "+name, flag.ContinueOnError)
		sfs.SetOutput(d.Stderr)
		sfs.Usage = fs.Usage
		sfs.StringVar(&o.dir, "C", o.dir, "the directory (default the working directory)")
		return sfs
	}
	switch sub {
	case "", "reload":
		if len(args) > 0 {
			return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
		}
	case "exec":
		sfs := subFlags(sub)
		if err := sfs.Parse(args); err != nil {
			return err
		}
		if args = sfs.Args(); len(args) == 0 {
			return nil
		}
		return d.pathenvExec(ctx, o, args)
	case "status":
		sfs := subFlags(sub)
		sfs.BoolVar(&o.json, "json", false, "print JSON")
		if err := sfs.Parse(args); err != nil {
			return err
		}
		if sfs.NArg() > 0 {
			return fmt.Errorf("status: unexpected arguments: %s", strings.Join(sfs.Args(), " "))
		}
	case "help":
		fs.Usage()
		return nil
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %s", sub)
	}

	if o.shell != "bash" {
		return fmt.Errorf("shell %q is not supported: only bash (set -shell or $SHELL)", o.shell)
	}
	dir, err := pathenvDir(o.dir)
	if err != nil {
		return err
	}

	set := d.loadPathenvSet(o.shell)
	m, err := set.match(dir)
	if err != nil {
		return err
	}
	active := strings.Fields(os.Getenv("PATHENV_ACTIVE"))
	if sub == "status" {
		return d.pathenvStatus(o, dir, m, active)
	}
	if m == nil {
		if !o.quiet {
			fmt.Fprintf(d.Stderr, "pathenv: no configuration for %s\n", dir)
		}
		return nil
	}
	entries, _, err := m.entries()
	if err != nil {
		return err
	}
	code, err := renderPathenvBash(planPathenv(m.file.name, entries, active, sub == "reload"))
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(d.Stdout, code)
	return err
}

// pathenvDir returns dir as an absolute, clean path; the empty dir is the
// working directory, as $PWD says if it is current.
func pathenvDir(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

// loadPathenvSet loads the configs of the repositories and their pathenv
// files, and reports the errors of those that fail to load. They are left
// out; the others still apply.
func (d *Dotfiles) loadPathenvSet(shell string) *pathenvSet {
	repos := d.loadRepos()
	// loadRepos reported its errors; a repository that fails to load only
	// loses its pathenv files.
	d.runErrors = nil
	set := loadPathenv(repos, d.DstPath, shell)
	for _, err := range set.loadErrors() {
		fmt.Fprintf(d.Stderr, "pathenv: %s\n", strings.ReplaceAll(err.Error(), "\n", "\n  "))
	}
	return set
}

// pathenvStatusJSON is the output of pathenv status -json.
type pathenvStatusJSON struct {
	Dir           string            `json:"dir"`
	Configuration *pathenvFileJSON  `json:"configuration"` // null if none matches
	Includes      []pathenvFileJSON `json:"includes,omitempty"`
	Location      *pathenvLocation  `json:"location,omitempty"`
	IsActive      bool              `json:"is_active"`
	Active        []string          `json:"active"` // PATHENV_ACTIVE
}

type pathenvFileJSON struct {
	Name string `json:"name"`
	File string `json:"file"`
}

func (d *Dotfiles) pathenvStatus(o pathenvOptions, dir string, m *pathenvMatch, active []string) error {
	st := pathenvStatusJSON{Dir: dir, Active: active}
	if st.Active == nil {
		st.Active = []string{}
	}
	if m != nil {
		_, includes, err := m.entries()
		if err != nil {
			return err
		}
		st.Configuration = &pathenvFileJSON{m.file.name, m.file.path}
		for _, f := range includes {
			st.Includes = append(st.Includes, pathenvFileJSON{f.name, f.path})
		}
		st.Location = &m.loc
		for _, a := range active {
			st.IsActive = st.IsActive || a == m.file.name
		}
	}
	if o.json {
		enc := json.NewEncoder(d.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	w := d.Stdout
	if m == nil {
		fmt.Fprintf(w, "configuration: none for %s\n", dir)
	} else {
		state := "not active"
		if st.IsActive {
			state = "active"
		}
		fmt.Fprintf(w, "configuration: %s (%s), %s\n", m.file.name, state, m.file.path)
		for _, f := range st.Includes {
			fmt.Fprintf(w, "include:       %s, %s\n", f.Name, f.File)
		}
		fmt.Fprintf(w, "prefix:        %s\n", m.loc.Prefix)
		fmt.Fprintf(w, "root:          %s\n", m.loc.Root)
		if m.loc.Worktree != "" {
			fmt.Fprintf(w, "worktree:      %s\n", m.loc.Worktree)
		}
	}
	fmt.Fprintf(w, "active:        %s\n", strings.Join(active, " "))
	return nil
}

// pathenvExecScript is the bash code of pathenv exec. Its arguments are the
// directory and the command; the dotfiles command that prints the code
// replaces DOTFILES. ~/.bashrc stops at the init's non-interactive return
// point. The variables are prefixed so that ~/.bashrc does not clobber
// them.
const pathenvExecScript = `__pathenv_dir=$1
shift
[ ! -f "$HOME/.bashrc" ] || PATHENV_SKIP=1 . "$HOME/.bashrc"
builtin cd -- "$__pathenv_dir" || exit
__pathenv_code=$(DOTFILES pathenv -shell bash -q) || exit
unset __pathenv_dir
eval "$__pathenv_code"
unset __pathenv_code
exec "$@"
`

// pathenvExec runs the command args in the environment of o.dir, with bash
// (see execBash).
func (d *Dotfiles) pathenvExec(ctx context.Context, o pathenvOptions, args []string) error {
	dir, err := pathenvDir(o.dir)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dotfiles := []string{exe, "-repos=" + d.ReposPath, "-dst=" + d.DstPath}
	if d.RepoList != "" {
		dotfiles = append(dotfiles, "-repository="+d.RepoList)
	}
	var quoted []string
	for _, a := range dotfiles {
		q, err := shellQuote(a)
		if err != nil {
			return err
		}
		quoted = append(quoted, q)
	}
	script := strings.Replace(pathenvExecScript, "DOTFILES", strings.Join(quoted, " "), 1)

	return d.execBash(ctx, append([]string{"bash", "-c", script, "pathenv-exec", dir}, args...))
}
