package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"strings"
)

// PullCommand runs dotfiles pull: Pull, and then InstallCommand unless a pull
// failed or -no-install is given.
func (d *Dotfiles) PullCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles pull", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles pull [options]\n\n")
		fmt.Fprintf(d.Stderr, "Pull all repositories in parallel, summarize the changes and then install,\n")
		fmt.Fprintf(d.Stderr, "unless a pull failed.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	noInstall := fs.Bool("no-install", false, "only pull; do not install afterwards")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("pull: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	switch {
	case !d.Pull():
		fmt.Fprintln(d.Stdout, "Not installing because a pull failed; run 'dotfiles install' to install anyway.")
	case !*noInstall:
		fmt.Fprintln(d.Stdout, "Installing...")
		d.InstallCommand()
	}
	return nil
}

// Pull pulls the main branch of all repositories from the upstream remote in
// parallel, prints a summary of what each pull brought in, or why it failed,
// and reports whether none failed. Repositories that are on another branch
// are skipped.
func (d *Dotfiles) Pull() bool {
	remote, err := d.pullRemote()
	if err != nil {
		d.fail(err)
		return false
	}
	var targets []remoteTarget
	for _, repoPath := range d.repos {
		targets = append(targets, newRemoteTarget(repoPath, remote, false))
	}
	return d.runRemotes(targets, nil, d.pullFrom)
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

// pullFrom pulls the main branch of t's repository from t's remote, unless
// another branch is checked out, which pulling would merge or rebase.
func (d *Dotfiles) pullFrom(ctx context.Context, t remoteTarget) remoteResult {
	branch := getMainBranch(t.repoPath)
	if current, _ := gitText(t.repoPath, "symbolic-ref", "-q", "--short", "HEAD"); current != branch {
		return remoteResult{target: t, skip: fmt.Sprintf("on %s, not %s", cmp.Or(current, "a detached HEAD"), branch)}
	}
	// before is empty in a repository that had no commits.
	before, _ := gitText(t.repoPath, "rev-parse", "-q", "--verify", "HEAD")
	if _, err := d.runTargetGit(ctx, t, "pull", "-q", t.remote, branch); err != nil {
		return remoteResult{target: t, err: err}
	}

	res := remoteResult{target: t}
	upstream := "refs/remotes/" + t.remote + "/" + branch
	if _, err := gitText(t.repoPath, "rev-parse", "-q", "--verify", upstream); err == nil {
		// Count the commits that came from the remote, those on its branch
		// that HEAD lacked, rather than what HEAD gained, which includes the
		// local commits that pulling rebased, or a merge commit.
		res.commits, res.stat = changesBetween(t.repoPath, before, upstream)
		res.unpushed = countCommits(t.repoPath, upstream+"..HEAD")
	} else {
		// Without a remote-tracking branch, count what HEAD gained.
		res.commits, res.stat = changesBetween(t.repoPath, before, "HEAD")
	}
	return res
}
