package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

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
	repos     []string
	runErrors []error
	errMu     sync.Mutex
}

func hasChanges(path string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

func hasDiff(path string) (bool, error) {
	cmd := exec.Command("git", "diff", "--quiet")
	cmd.Dir = path
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ExitCode() == 1 {
			return true, nil
		}
	}
	return false, err
}

func (d *Dotfiles) collectErr(err error) {
	if err == nil {
		return
	}
	d.errMu.Lock()
	defer d.errMu.Unlock()
	d.runErrors = append(d.runErrors, err)
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
		fmt.Fprintf(stderr, "  push      Run git push origin master on all repos in parallel\n")
		fmt.Fprintf(stderr, "  publish   Run git push publish master on all repos in parallel\n")
		fmt.Fprintf(stderr, "  pull      Run git fetch origin && git merge origin/master on all repos in parallel\n")
		fmt.Fprintf(stderr, "  status, s Run git status on all repos (only if changes exist)\n")
		fmt.Fprintf(stderr, "  diff, d   Run git diff on all repos\n")
		fmt.Fprintf(stderr, "  magit     Open magit-status in emacs for all repos with changes\n")
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
	fs.BoolVar(&d.DryRun, "dry-run", false, "dry run (don't create symlinks; only for install command)")
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

	if err := d.discoverRepos(); err != nil {
		return err
	}

	switch command {
	case "push":
		d.Push()
	case "publish":
		d.Publish()
	case "pull":
		d.Pull()
		fmt.Fprintln(d.Stdout, "Installing...")
		d.Install()
	case "status", "s":
		d.Status()
	case "diff", "d":
		d.Diff()
	case "magit":
		d.Magit()
	case "install", "i":
		d.Install()
	case "plan":
		d.Plan()
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", command)
		fs.Usage()
		return fmt.Errorf("unknown command: %s", command)
	}

	if len(d.runErrors) > 0 {
		fmt.Fprintf(stderr, "\n--- Summary of errors (%d) ---\n", len(d.runErrors))
		for _, err := range d.runErrors {
			fmt.Fprintf(stderr, "  - %v\n", err)
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

func (d *Dotfiles) Push() {
	var wg sync.WaitGroup
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		wg.Add(1)
		go func(name, path string) {
			defer wg.Done()
			action := &ExecCommandAction{
				SrcRoot: path,
				Command: "git",
				Args:    []string{"push", "-q", "origin", "master"},
			}
			fmt.Fprintln(d.Stdout, action.String())
			if err := action.Run(); err != nil {
				fmt.Fprintf(d.Stderr, "error in %s: %v\n", name, err)
				d.collectErr(fmt.Errorf("%s: %w", action.String(), err))
			}
		}(name, repoPath)
	}
	wg.Wait()
}

func (d *Dotfiles) Publish() {
	var wg sync.WaitGroup
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		wg.Add(1)
		go func(name, path string) {
			defer wg.Done()

			cmd := exec.Command("git", "remote")
			cmd.Dir = path
			output, err := cmd.Output()
			if err != nil {
				fmt.Fprintf(d.Stderr, "error checking remotes in %s: %v\n", name, err)
				return
			}

			hasPublish := false
			for _, remote := range strings.Split(string(output), "\n") {
				if strings.TrimSpace(remote) == "publish" {
					hasPublish = true
					break
				}
			}

			if !hasPublish {
				fmt.Fprintf(d.Stdout, "skipping publish for %s: remote 'publish' not found\n", name)
				return
			}

			action := &ExecCommandAction{
				SrcRoot: path,
				Command: "git",
				Args:    []string{"push", "-q", "publish", "master"},
			}
			fmt.Fprintln(d.Stdout, action.String())
			if err := action.Run(); err != nil {
				fmt.Fprintf(d.Stderr, "error in %s: %v\n", name, err)
				d.collectErr(fmt.Errorf("%s: %w", action.String(), err))
			}
		}(name, repoPath)
	}
	wg.Wait()
}

func (d *Dotfiles) Pull() {
	var wg sync.WaitGroup
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		wg.Add(1)
		go func(name, path string) {
			defer wg.Done()
			actions := []Action{
				&ExecCommandAction{
					SrcRoot: path,
					Command: "git",
					Args:    []string{"fetch", "-q", "origin"},
				},
				&ExecCommandAction{
					SrcRoot: path,
					Command: "git",
					Args:    []string{"merge", "-q", "origin/master"},
				},
			}

			for _, action := range actions {
				fmt.Fprintln(d.Stdout, action.String())
				if err := action.Run(); err != nil {
					fmt.Fprintf(d.Stderr, "error in %s: %v\n", name, err)
					d.collectErr(fmt.Errorf("%s: %w", action.String(), err))
				}
			}
		}(name, repoPath)
	}
	wg.Wait()
}

func (d *Dotfiles) Status() {
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		changed, err := hasChanges(repoPath)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error checking status for %s: %v\n", name, err)
			d.collectErr(fmt.Errorf("status %s: %w", name, err))
			continue
		}
		if changed {
			fmt.Fprintf(d.Stdout, "[- %s ]\n", name)
			cmd := exec.Command("git", "status", "--short", "--branch")
			cmd.Dir = repoPath
			cmd.Stdout = d.Stdout
			cmd.Stderr = d.Stderr
			cmd.Run()
		}
	}
}

func (d *Dotfiles) Diff() {
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		changed, err := hasDiff(repoPath)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error checking diff for %s: %v\n", name, err)
			d.collectErr(fmt.Errorf("diff %s: %w", name, err))
			continue
		}
		if changed {
			fmt.Fprintf(d.Stdout, "[- %s ]\n", name)
			cmd := exec.Command("git", "diff")
			cmd.Dir = repoPath
			cmd.Stdout = d.Stdout
			cmd.Stderr = d.Stderr
			cmd.Run()
		}
	}
}

func (d *Dotfiles) Magit() {
	var magitArgs []string
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		changed, err := hasChanges(repoPath)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error checking status for %s: %v\n", name, err)
			d.collectErr(fmt.Errorf("magit %s: %w", name, err))
			continue
		}
		if changed {
			absPath, _ := filepath.Abs(repoPath)
			magitArgs = append(magitArgs, fmt.Sprintf("(magit-status \"%s/\")", absPath))
		}
	}

	if len(magitArgs) == 0 {
		fmt.Fprintln(d.Stdout, "no changes")
	} else {
		evalStr := fmt.Sprintf("(progn %s (delete-other-windows) )", strings.Join(magitArgs, " "))
		cmd := exec.Command("emacs", "-eval", evalStr)
		cmd.Stdout = d.Stdout
		cmd.Stderr = d.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(d.Stderr, "error: %v\n", err)
			d.collectErr(fmt.Errorf("magit: %w", err))
		}
	}
}

func (d *Dotfiles) Install() { d.sync(false) }
func (d *Dotfiles) Plan()    { d.sync(true) }

func (d *Dotfiles) sync(isPlan bool) {
	var (
		allActions     []Action
		allGoInstall   []Action
		allPreScripts  []Action
		allPostScripts []Action
	)

	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		r := NewRepository(repoPath, d.DstPath, WithForce(d.Force), WithCopy(d.Copy))
		if err := r.LoadConfig(); err != nil {
			if errors.Is(err, ErrConfigMissing) {
				continue
			}
			fmt.Fprintf(d.Stderr, "error loading config for %s: %v\n", repoPath, err)
			d.collectErr(fmt.Errorf("config %s: %w", name, err))
			continue
		}

		actions, err := r.Sync()
		if err != nil {
			fmt.Fprintf(d.Stderr, "error planning %s: %v\n", repoPath, err)
			d.collectErr(fmt.Errorf("plan %s: %w", name, err))
			continue
		}
		allActions = append(allActions, actions...)

		allPreScripts = append(allPreScripts, r.PreScript()...)
		allPostScripts = append(allPostScripts, r.PostScript()...)
		allGoInstall = append(allGoInstall, r.GoInstall()...)
	}

	for _, script := range slices.Concat(allGoInstall, allPreScripts, allActions, allPostScripts) {
		if isPlan {
			fmt.Fprintf(d.Stdout, "plan: %s\n", script.String())
			continue
		}
		if d.Verbose {
			fmt.Fprintf(d.Stdout, "%s\n", script.String())
		}
		if err := script.Run(); err != nil {
			if d.Verbose {
				fmt.Fprintf(d.Stderr, "error: %v\n", err)
			}
			d.collectErr(fmt.Errorf("%s: %w", script.String(), err))
		}
	}
}

func main() {
	d := &Dotfiles{}
	if err := d.Run(os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
