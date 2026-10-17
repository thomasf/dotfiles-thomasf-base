package main

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// PushCommand pushes the main branch of all repositories to their private and
// backup remotes and then prints a summary of what each push sent, or why it
// failed. Backup remotes that cannot be pushed to are skipped with a warning.
func (d *Dotfiles) PushCommand() {
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}

	var (
		targets []remoteTarget
		decided []remoteResult
		skipped = make(map[string][]string) // reason -> repository names
	)
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		repoTargets, err := pushTargets(set, repoPath)
		if err != nil {
			decided = append(decided, d.remotesError(repoPath, err))
			continue
		}
		for _, t := range repoTargets {
			if reason := backupSkipReason(t); reason != "" {
				skipped[reason] = append(skipped[reason], name)
				continue
			}
			targets = append(targets, t)
		}
	}

	for _, reason := range slices.Sorted(maps.Keys(skipped)) {
		names := skipped[reason]
		fmt.Fprintf(d.Stderr, "warning: %s, skipped for %d repos: %s\n", reason, len(names), strings.Join(names, ", "))
	}
	d.runRemotes(targets, decided, d.pushTo)
}

// backupSkipReason returns why t, if it is a backup, cannot be pushed to, or
// "" if it can.
func backupSkipReason(t remoteTarget) string {
	if !t.backup {
		return ""
	}
	if t.url == "" {
		return fmt.Sprintf("remote %s is not configured (run 'dotfiles repos fix')", t.remote)
	}
	switch localBackupState(t.url) {
	case backupNotCreated:
		return fmt.Sprintf("remote %s repository does not exist (run 'dotfiles repos fix')", t.remote)
	case backupUnavailable:
		return fmt.Sprintf("remote %s is unavailable", t.remote)
	}
	return ""
}

// pushTo pushes the main branch of t's repository to t's remote.
func (d *Dotfiles) pushTo(ctx context.Context, t remoteTarget) remoteResult {
	out, err := d.runTargetGit(ctx, t, "push", "--porcelain", t.remote, getMainBranch(t.repoPath))
	if err != nil {
		return remoteResult{target: t, err: err}
	}
	note, from, to := parsePushPorcelain(out)
	commits, stat := changesBetween(t.repoPath, from, to)
	return remoteResult{target: t, note: note, commits: commits, stat: stat}
}

// parsePushPorcelain returns what the output out of git push --porcelain
// reports for the pushed branch: a note for a new branch or a forced update,
// and the commits the branch moved from and to, if it moved.
func parsePushPorcelain(out string) (note, from, to string) {
	for line := range strings.Lines(out) {
		// A ref line is: flag TAB from:to TAB summary.
		fields := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(fields) != 3 {
			continue
		}
		switch flag, summary := fields[0], fields[2]; flag {
		case "*":
			return "new branch", "", ""
		case "+": // old...new (forced update)
			from, rest, _ := strings.Cut(summary, "...")
			to, _, _ := strings.Cut(rest, " ")
			return "forced update", from, to
		case " ": // old..new
			from, to, _ := strings.Cut(summary, "..")
			return "", from, to
		}
	}
	return "", "", ""
}

// pushTargets returns the targets for pushing the repository at path: the
// active private and backup remotes, or without configured remotes, origin
// and backup.* from the repository itself.
func pushTargets(set *repoSet, path string) ([]remoteTarget, error) {
	var targets []remoteTarget
	add := func(remote string, backup bool) {
		t := newRemoteTarget(path, remote, true)
		t.backup = backup
		targets = append(targets, t)
	}
	if set.configured() {
		for _, r := range set.active(RemoteKindPrivate, RemoteKindBackup) {
			add(r.Name, r.Kind == RemoteKindBackup)
		}
		return targets, nil
	}
	names, err := getPushRemotes(path)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		add(name, strings.HasPrefix(name, "backup."))
	}
	return targets, nil
}

// getPushRemotes returns origin and the backup.* remotes of the repository at
// path, or origin if it has neither.
func getPushRemotes(path string) ([]string, error) {
	out, err := gitText(path, "remote")
	if err != nil {
		return nil, err
	}
	var remotes []string
	for _, remote := range strings.Fields(out) {
		if remote == "origin" || strings.HasPrefix(remote, "backup.") {
			remotes = append(remotes, remote)
		}
	}
	if len(remotes) == 0 {
		return []string{"origin"}, nil
	}
	return remotes, nil
}
