package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// PublishCommand pushes the main branch of all public repositories to their
// publish remotes and then prints a summary of what each push sent, or why it
// failed.
func (d *Dotfiles) PublishCommand() {
	set, err := d.loadRepoSet()
	if err != nil {
		d.fail(err)
		return
	}
	publishRemotes := []string{"publish"}
	if set.configured() {
		publishRemotes = nil
		for _, r := range set.active(RemoteKindPublish) {
			publishRemotes = append(publishRemotes, r.Name)
		}
	}

	var (
		targets []remoteTarget
		decided []remoteResult
	)
	for _, repoPath := range d.repos {
		name := filepath.Base(repoPath)
		remotes, err := gitRemotes(repoPath)
		if err != nil {
			decided = append(decided, d.remotesError(repoPath, err))
			continue
		}
		if !set.public(repoPath) {
			if v := set.publishViolations(repoPath, remotes); len(v) > 0 {
				remote := strings.Join(v, ", ")
				d.collectErr(fmt.Errorf("%s: publish remote %s on a repository that is not public", name, remote))
				t := remoteTarget{repoPath: repoPath, remote: remote}
				decided = append(decided, remoteResult{target: t, err: errors.New("publish remote on a repository that is not public")})
			} else if d.Verbose {
				fmt.Fprintf(d.Stdout, "skipping publish for %s: repository is not public\n", name)
			}
			continue
		}
		for _, remote := range publishRemotes {
			t := newRemoteTarget(repoPath, remote, true)
			if t.url == "" {
				decided = append(decided, remoteResult{target: t, skip: "remote not found"})
				continue
			}
			targets = append(targets, t)
		}
	}
	d.runRemotes(targets, decided, d.pushTo)
}
