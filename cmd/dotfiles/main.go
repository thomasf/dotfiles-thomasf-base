package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peterbourgon/ff/v3"
)

type Dotfiles struct {
	Stdout io.Writer
	Stderr io.Writer

	// Options
	RepoList  string
	ReposPath string
	DstPath   string
	Force     bool
	Copy      bool
	DryRun    bool
	Verbose   bool

	// Internal state
	repos            []string
	runErrors        []error
	errMu            sync.Mutex
	hostCheckTimeout time.Duration // of checkSSHHosts; zero means defaultHostCheckTimeout
	interrupted      atomic.Bool   // SIGINT or SIGTERM stopped runRemotes
}

func (d *Dotfiles) collectErr(err error) {
	if err == nil {
		return
	}
	d.errMu.Lock()
	defer d.errMu.Unlock()
	d.runErrors = append(d.runErrors, err)
}

func (d *Dotfiles) fail(err error) {
	fmt.Fprintf(d.Stderr, "error: %v\n", err)
	d.collectErr(err)
}

func (d *Dotfiles) Run(stdout, stderr io.Writer, args []string) error {
	d.Stdout = stdout
	d.Stderr = stderr

	fs := flag.NewFlagSet("dotfiles", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: dotfiles [options] <command>\n")
		fmt.Fprintf(stderr, "\nCommands:\n")
		fmt.Fprintf(stderr, "  install   Synchronize dotfiles (create symlinks)\n")
		fmt.Fprintf(stderr, "  plan      Print synchronization plan without taking action\n")
		fmt.Fprintf(stderr, "  push      Run git push on all repos in parallel\n")
		fmt.Fprintf(stderr, "  publish   Run git push publish on all repos in parallel\n")
		fmt.Fprintf(stderr, "  pull [options]  Run git pull origin on all repos in parallel, summarize the changes and install (see pull -h)\n")
		fmt.Fprintf(stderr, "  status, s Run git status on all repos (only if changes exist)\n")
		fmt.Fprintf(stderr, "  diff, d   Run git diff on all repos\n")
		fmt.Fprintf(stderr, "  magit     Open magit-status in emacs for all repos with changes\n")
		fmt.Fprintf(stderr, "  log [options]   Show the commits of all repos as one log, newest first (see log -h)\n")
		fmt.Fprintf(stderr, "  repos [status]  Show known repos and the state of their remotes\n")
		fmt.Fprintf(stderr, "  repos fix       Add missing remotes, correct remote URLs and create missing local backup repos\n")
		fmt.Fprintf(stderr, "  repos clone [name...]  Clone missing known repos (all, or the named ones) from the upstream remote, then run repos fix\n")
		fmt.Fprintf(stderr, "  doc [topic]     Print the manual, or only the sections whose heading or number matches topic; doc toc lists them\n")
		fmt.Fprintf(stderr, "\nOptions:\n")
		fs.PrintDefaults()
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not get home directory: %w", err)
	}

	fs.StringVar(&d.RepoList, "repository", "", "comma-separated list of dotfiles repositories")
	fs.StringVar(&d.ReposPath, "repos", filepath.Join(home, "src", "dotfiles"), "repositories path (defaults to $HOME/src/dotfiles)")
	fs.StringVar(&d.DstPath, "dst", home, "target path (defaults to $HOME)")
	fs.BoolVar(&d.Force, "f", false, "force overwrite existing files")
	fs.BoolVar(&d.Copy, "copy", false, "copy files instead of symlinking")
	fs.BoolVar(&d.DryRun, "dry-run", false, "dry run (print actions without running them; only for repos fix and repos clone)")
	fs.BoolVar(&d.Verbose, "v", false, "log more non errors")

	if err := ff.Parse(fs, args, ff.WithEnvVarPrefix("DOTFILES")); err != nil {
		return err
	}

	remainingArgs := fs.Args()
	if len(remainingArgs) == 0 {
		fs.Usage()
		return errors.New("no command specified")
	}

	command := remainingArgs[0]
	if command == "doc" {
		return d.DocCommand(remainingArgs[1:])
	}

	if err := d.discoverRepos(); err != nil {
		return err
	}

	switch command {
	case "push":
		d.PushCommand()
	case "publish":
		d.PublishCommand()
	case "pull":
		if err := d.PullCommand(remainingArgs[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
	case "status", "s":
		d.StatusCommand()
	case "diff", "d":
		d.DiffCommand()
	case "magit":
		d.MagitCommand()
	case "log":
		if err := d.LogCommand(remainingArgs[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
	case "repos":
		if err := d.ReposCommand(remainingArgs[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			fs.Usage()
			return err
		}
	case "install", "i":
		d.InstallCommand()
	case "plan":
		d.PlanCommand()
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", command)
		fs.Usage()
		return fmt.Errorf("unknown command: %s", command)
	}

	if len(d.runErrors) > 0 {
		fmt.Fprintf(stderr, "\n--- Summary of errors (%d) ---\n", len(d.runErrors))
		for _, err := range d.runErrors {
			fmt.Fprintf(stderr, "  - %s\n", strings.ReplaceAll(err.Error(), "\n", "\n    "))
		}
		return errors.New("some commands failed")
	}

	return nil
}

func (d *Dotfiles) discoverRepos() error {
	if d.RepoList != "" {
		d.repos = strings.Split(d.RepoList, ",")
		return nil
	}

	dotfilesSrc := d.ReposPath
	entries, err := os.ReadDir(dotfilesSrc)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("no repositories found or specified")
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			d.repos = append(d.repos, filepath.Join(dotfilesSrc, entry.Name()))
		}
	}

	if len(d.repos) == 0 {
		return errors.New("no repositories found or specified")
	}
	return nil
}

func main() {
	d := &Dotfiles{}
	if err := d.Run(os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
