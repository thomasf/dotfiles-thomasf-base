package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
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
		fmt.Fprintf(stderr, "  push      Run git push on all repos in parallel\n")
		fmt.Fprintf(stderr, "  publish   Run git push publish on all repos in parallel\n")
		fmt.Fprintf(stderr, "  pull      Run git pull origin on all repos in parallel\n")
		fmt.Fprintf(stderr, "  status, s Run git status on all repos (only if changes exist)\n")
		fmt.Fprintf(stderr, "  diff, d   Run git diff on all repos\n")
		fmt.Fprintf(stderr, "  magit     Open magit-status in emacs for all repos with changes\n")
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
		return d.Doc(remainingArgs[1:])
	}

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
	case "repos":
		if err := d.Repos(remainingArgs[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			fs.Usage()
			return err
		}
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
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		skipped = make(map[string][]string) // reason -> repository names
	)
	skip := func(reason, name string) {
		mu.Lock()
		defer mu.Unlock()
		skipped[reason] = append(skipped[reason], name)
	}

	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		wg.Go(func() {
			targets, err := d.pushTargets(set, repoPath)
			if err != nil {
				fmt.Fprintf(d.Stderr, "error finding remotes in %s: %v\n", name, err)
				return
			}
			remotes, err := gitRemotes(repoPath)
			if err != nil {
				fmt.Fprintf(d.Stderr, "error finding remotes in %s: %v\n", name, err)
				return
			}

			branch := d.getMainBranch(repoPath)
			for _, target := range targets {
				if target.backup {
					remote, ok := remotes[target.name]
					if !ok {
						skip(fmt.Sprintf("remote %s is not configured (run 'dotfiles repos fix')", target.name), name)
						continue
					}
					switch localBackupState(remote.pushTo()) {
					case backupNotCreated:
						skip(fmt.Sprintf("remote %s repository does not exist (run 'dotfiles repos fix')", target.name), name)
						continue
					case backupUnavailable:
						skip(fmt.Sprintf("remote %s is unavailable", target.name), name)
						continue
					}
				}

				action := &ExecCommandAction{
					SrcRoot: repoPath,
					Command: "git",
					Args:    []string{"push", "-q", target.name, branch},
				}
				fmt.Fprintln(d.Stdout, action.String())
				if err := action.Run(); err != nil {
					if target.backup {
						fmt.Fprintf(d.Stderr, "warning: %s (%s): %v\n", name, target.name, err)
						continue
					}
					fmt.Fprintf(d.Stderr, "error in %s (%s): %v\n", name, target.name, err)
					d.collectErr(fmt.Errorf("%s: %w", action.String(), err))
				}
			}
		})
	}
	wg.Wait()

	for _, reason := range slices.Sorted(maps.Keys(skipped)) {
		names := skipped[reason]
		slices.Sort(names)
		fmt.Fprintf(d.Stderr, "warning: %s, skipped for %d repos: %s\n", reason, len(names), strings.Join(names, ", "))
	}
}

type pushTarget struct {
	name   string
	backup bool // failures are warnings, not errors
}

// pushTargets returns the active private and backup remotes, or without
// configured remotes, origin and backup.* from the repository itself.
func (d *Dotfiles) pushTargets(set *repoSet, path string) ([]pushTarget, error) {
	var targets []pushTarget
	if set.configured() {
		for _, r := range set.active(RemoteKindPrivate, RemoteKindBackup) {
			targets = append(targets, pushTarget{name: r.Name, backup: r.Kind == RemoteKindBackup})
		}
		return targets, nil
	}
	names, err := d.getPushRemotes(path)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		targets = append(targets, pushTarget{name: name, backup: strings.HasPrefix(name, "backup.")})
	}
	return targets, nil
}

func (d *Dotfiles) getPushRemotes(path string) ([]string, error) {
	cmd := exec.Command("git", "remote")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var remotes []string
	for _, line := range lines {
		remote := strings.TrimSpace(line)
		if remote == "" {
			continue
		}

		if remote == "origin" || strings.HasPrefix(remote, "backup.") {
			remotes = append(remotes, remote)
		}
	}

	if len(remotes) == 0 {
		return []string{"origin"}, nil
	}

	return remotes, nil
}

func (d *Dotfiles) getMainBranch(path string) string {
	cmd := exec.Command("git", "branch", "--list", "main", "master")
	cmd.Dir = path
	output, _ := cmd.Output()
	lines := strings.SplitSeq(strings.TrimSpace(string(output)), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		if line == "main" || line == "master" {
			return line
		}
	}
	return "master"
}

func (d *Dotfiles) Publish() {
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}
	targets := []string{"publish"}
	if set.configured() {
		targets = nil
		for _, r := range set.active(RemoteKindPublish) {
			targets = append(targets, r.Name)
		}
	}

	var wg sync.WaitGroup
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		wg.Go(func() {
			remotes, err := gitRemotes(repoPath)
			if err != nil {
				fmt.Fprintf(d.Stderr, "error checking remotes in %s: %v\n", name, err)
				return
			}

			if !set.public(repoPath) {
				if v := set.publishViolations(repoPath, remotes); len(v) > 0 {
					d.fail(fmt.Errorf("%s: publish remote %s on a repository that is not public", name, strings.Join(v, ", ")))
					return
				}
				if d.Verbose {
					fmt.Fprintf(d.Stdout, "skipping publish for %s: repository is not public\n", name)
				}
				return
			}

			branch := d.getMainBranch(repoPath)
			for _, remote := range targets {
				if _, ok := remotes[remote]; !ok {
					fmt.Fprintf(d.Stdout, "skipping publish for %s: remote '%s' not found\n", name, remote)
					continue
				}
				action := &ExecCommandAction{
					SrcRoot: repoPath,
					Command: "git",
					Args:    []string{"push", "-q", remote, branch},
				}
				fmt.Fprintln(d.Stdout, action.String())
				if err := action.Run(); err != nil {
					fmt.Fprintf(d.Stderr, "error in %s: %v\n", name, err)
					d.collectErr(fmt.Errorf("%s: %w", action.String(), err))
				}
			}
		})
	}
	wg.Wait()
}

func (d *Dotfiles) Pull() {
	remote, err := d.pullRemote()
	if err != nil {
		d.fail(err)
		return
	}

	var wg sync.WaitGroup
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		wg.Go(func() {
			branch := d.getMainBranch(repoPath)
			action := &ExecCommandAction{
				SrcRoot: repoPath,
				Command: "git",
				Args:    []string{"pull", "-q", remote, branch},
			}

			fmt.Fprintln(d.Stdout, action.String())
			if err := action.Run(); err != nil {
				fmt.Fprintf(d.Stderr, "error in %s: %v\n", name, err)
				d.collectErr(fmt.Errorf("%s: %w", action.String(), err))
			}
		})
	}
	wg.Wait()
}

// pullRemote returns the upstream remote, or origin when no remotes are
// configured.
func (d *Dotfiles) pullRemote() (string, error) {
	set, err := d.loadRepoSet()
	if err != nil {
		return "", err
	}
	if !set.configured() {
		return "origin", nil
	}
	r, err := set.upstream()
	return r.Name, err
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
		prefix := "magit: "
		cmd.Stdout = newPrefixWriter(d.Stdout, prefix)
		cmd.Stderr = newPrefixWriter(d.Stderr, prefix)
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

	for _, r := range d.loadRepos(WithForce(d.Force), WithCopy(d.Copy)) {
		name := filepath.Base(r.srcRoot)
		actions, err := r.Sync()
		if err != nil {
			fmt.Fprintf(d.Stderr, "error planning %s: %v\n", r.srcRoot, err)
			d.collectErr(fmt.Errorf("plan %s: %w", name, err))
			continue
		}
		allActions = append(allActions, actions...)

		allPreScripts = append(allPreScripts, r.PreScript()...)
		allPostScripts = append(allPostScripts, r.PostScript()...)
		allGoInstall = append(allGoInstall, r.GoInstall()...)
	}

	all := slices.Concat(allGoInstall, allPreScripts, allActions, allPostScripts)

	// Check for multiple actions targeting the same destination path
	destMap := make(map[string][]Action)
	for _, action := range all {
		if t, ok := action.(Targeter); ok {
			dst := t.DstAbsolutePath()
			if dst != "" {
				destMap[dst] = append(destMap[dst], action)
			}
		}
	}

	for dst, actions := range destMap {
		if len(actions) > 1 {
			fmt.Fprintf(d.Stderr, "Warning: multiple actions target the same destination: %s\n", dst)
			for _, action := range actions {
				fmt.Fprintf(d.Stderr, "  - %s\n", action.String())
			}
		}
	}

	for _, script := range all {
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
