package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
)

// ReposCommand runs a repos subcommand.
func (d *Dotfiles) ReposCommand(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status", "s", "fix":
		if len(args) > 1 {
			return fmt.Errorf("repos %s takes no arguments", sub)
		}
		if sub == "fix" {
			d.ReposFixCommand()
		} else {
			d.ReposStatusCommand()
		}
	case "clone":
		d.ReposCloneCommand(args[1:])
	default:
		return fmt.Errorf("unknown repos command: %s", sub)
	}
	return nil
}

// ReposStatusCommand prints a table of every known repository and the state of
// each configured remote.
func (d *Dotfiles) ReposStatusCommand() {
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}
	if !set.configured() && len(set.names) == 0 {
		d.fail(errors.New("no [repositories] configuration found"))
		return
	}

	tw := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	header := []string{"REPO"}
	for _, r := range set.remotes {
		header = append(header, r.Name)
	}
	header = append(header, "NOTES")
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	for _, e := range d.repoEntries(set) {
		row := []string{e.name}
		var notes []string
		if !e.listed {
			notes = append(notes, "not in names")
		}
		if !e.cloned {
			for range set.remotes {
				row = append(row, "-")
			}
			notes = append(notes, "not cloned")
			fmt.Fprintln(tw, strings.Join(append(row, strings.Join(notes, "; ")), "\t"))
			continue
		}

		remotes, err := gitRemotes(e.path)
		if err != nil {
			d.collectErr(fmt.Errorf("%s: %w", e.name, err))
			remotes = nil
			notes = append(notes, fmt.Sprintf("error: %v", err))
		}
		public := set.public(e.path)
		for _, r := range set.remotes {
			row = append(row, remoteStatus(r, e.name, remotes, public))
		}
		if v := set.publishViolations(e.path, remotes); len(v) > 0 {
			d.collectErr(fmt.Errorf("%s: publish remote %s on a repository that is not public", e.name, strings.Join(v, ", ")))
		}
		var extra []string
		for _, name := range slices.Sorted(maps.Keys(remotes)) {
			if !slices.ContainsFunc(set.remotes, func(r remoteSpec) bool { return r.Name == name }) {
				extra = append(extra, name)
			}
		}
		if len(extra) > 0 {
			notes = append(notes, "extra: "+strings.Join(extra, ", "))
		}
		fmt.Fprintln(tw, strings.Join(append(row, strings.Join(notes, "; ")), "\t"))
	}
	tw.Flush()
}

func remoteStatus(r remoteSpec, repo string, remotes map[string]gitRemote, public bool) string {
	have, exists := remotes[r.Name]
	switch {
	case r.Kind == RemoteKindPublish && !public:
		if exists {
			return "ERROR: not public"
		}
		return "-"
	case !r.active:
		if exists {
			return "extra (inactive)"
		}
		return "inactive"
	case !exists:
		return "missing"
	case have.url != r.urlFor(repo) || have.pushURL != r.pushURLFor(repo):
		return "differs"
	}
	if r.Kind == RemoteKindBackup {
		switch localBackupState(have.pushTo()) {
		case backupNotCreated:
			return "not created"
		case backupUnavailable:
			return "unavailable"
		}
	}
	return "ok"
}

// ReposFixCommand adds missing remotes and corrects remote URLs in every
// cloned repository listed in repositories.names, and creates missing local
// backup repositories. It never removes remotes.
func (d *Dotfiles) ReposFixCommand() {
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}
	if !set.configured() {
		d.fail(errors.New("no remotes configured in [repositories.remote]"))
		return
	}
	d.fixAll(set)
}

func (d *Dotfiles) fixAll(set *repoSet) {
	for _, e := range d.repoEntries(set) {
		if e.cloned && e.listed {
			d.runActions(d.fixActions(set, e))
		}
	}
}

func (d *Dotfiles) fixActions(set *repoSet, e repoEntry) []Action {
	remotes, err := gitRemotes(e.path)
	if err != nil {
		d.fail(fmt.Errorf("%s: %w", e.name, err))
		return nil
	}
	if v := set.publishViolations(e.path, remotes); len(v) > 0 {
		d.fail(fmt.Errorf("%s: publish remote %s on a repository that is not public", e.name, strings.Join(v, ", ")))
	}

	git := func(args ...string) Action {
		return &ExecAction{SrcRoot: e.path, Name: "git", Args: args}
	}
	public := set.public(e.path)
	var actions []Action
	for _, r := range set.remotes {
		if !r.active || (r.Kind == RemoteKindPublish && !public) {
			continue
		}
		want := r.urlFor(e.name)
		wantPush := r.pushURLFor(e.name)
		if r.Kind == RemoteKindBackup {
			if p, ok := localPath(cmp.Or(wantPush, want)); ok && localBackupState(p) == backupNotCreated {
				actions = append(actions, git("init", "-q", "--bare", p))
			}
		}
		have, ok := remotes[r.Name]
		switch {
		case !ok:
			actions = append(actions, git("remote", "add", r.Name, want))
		case have.url != want:
			actions = append(actions, git("remote", "set-url", r.Name, want))
		}
		switch {
		case wantPush != "" && have.pushURL != wantPush:
			actions = append(actions, git("remote", "set-url", "--push", r.Name, wantPush))
		case wantPush == "" && have.pushURL != "":
			actions = append(actions, git("config", "--unset-all", "remote."+r.Name+".pushurl"))
		}
	}
	return actions
}

// ReposCloneCommand clones the repositories in repositories.names that are
// missing from the repositories path, or only those in names if any are given,
// from the upstream remote. It then fixes the remotes of every listed
// repository.
func (d *Dotfiles) ReposCloneCommand(names []string) {
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}
	upstream, err := set.upstream()
	if err != nil {
		d.fail(err)
		return
	}
	for _, name := range names {
		if !slices.Contains(set.names, name) {
			d.fail(fmt.Errorf("unknown repository %q: not in repositories.names", name))
			return
		}
	}

	var targets []remoteTarget
	for _, e := range d.repoEntries(set) {
		if e.cloned || !e.listed || (len(names) > 0 && !slices.Contains(names, e.name)) {
			continue
		}
		url := upstream.urlFor(e.name)
		if d.DryRun {
			fmt.Fprintf(d.Stdout, "plan: %s\n", gitDesc(e.path, cloneArgs(upstream.Name, url, e.path)...))
			continue
		}
		// The host check needs the URL that git connects to, after
		// url.<base>.insteadOf rewrites.
		resolved, _ := gitText(d.ReposPath, "ls-remote", "--get-url", url)
		targets = append(targets, remoteTarget{repoPath: e.path, remote: upstream.Name, url: cmp.Or(resolved, url)})
	}
	if len(targets) > 0 {
		d.runRemotes(targets, nil, func(ctx context.Context, t remoteTarget) remoteResult {
			// Clone from the configured URL, which repos fix expects.
			args := cloneArgs(t.remote, upstream.urlFor(filepath.Base(t.repoPath)), t.repoPath)
			if _, err := d.runTargetGit(ctx, t, args...); err != nil {
				return remoteResult{target: t, err: err}
			}
			return remoteResult{target: t, note: "cloned"}
		})
	}

	if d.interrupted.Load() {
		return
	}

	var cloned []string
	for _, t := range targets {
		if isDir(t.repoPath) {
			cloned = append(cloned, t.repoPath)
		}
	}

	if len(cloned) > 0 {
		// Reload so that the configs of the new repositories, which may
		// declare them public, are included.
		d.repos = append(d.repos, cloned...)
		set, err = d.loadRepoSet()
		if err != nil {
			d.fail(err)
			return
		}
	}
	d.fixAll(set)
}

// cloneArgs returns the git arguments for cloning url into dst, naming the
// remote remote.
func cloneArgs(remote, url, dst string) []string {
	return []string{"clone", "-q", "-o", remote, url, dst}
}

// runActions runs actions in order, or only prints them in dry-run mode.
func (d *Dotfiles) runActions(actions []Action) {
	for _, a := range actions {
		if d.DryRun {
			fmt.Fprintf(d.Stdout, "plan: %s\n", a)
			continue
		}
		fmt.Fprintln(d.Stdout, a.String())
		if err := a.Run(); err != nil {
			d.fail(fmt.Errorf("%s: %w", a, err))
		}
	}
}

// repoEntry is a repository known from the names list, from d.repos, or both.
type repoEntry struct {
	name   string
	path   string
	cloned bool
	listed bool // in repositories.names
}

func (d *Dotfiles) repoEntries(set *repoSet) []repoEntry {
	byName := make(map[string]*repoEntry)
	for _, path := range d.repos {
		name := filepath.Base(path)
		byName[name] = &repoEntry{name: name, path: path, cloned: isDir(path)}
	}
	for _, name := range set.names {
		e, ok := byName[name]
		if !ok {
			path := filepath.Join(d.ReposPath, name)
			e = &repoEntry{name: name, path: path, cloned: isDir(path)}
			byName[name] = e
		}
		e.listed = true
	}
	var entries []repoEntry
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		entries = append(entries, *byName[name])
	}
	return entries
}
