package main

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
)

// remoteSpec is a remote merged from the configs of all repositories.
type remoteSpec struct {
	Name string
	Remote
	source string      // config file the remote was first read from
	repo   *Repository // repository of that config
	active bool        // condition is true on this host
}

func (r remoteSpec) urlFor(repo string) string {
	return strings.ReplaceAll(r.URL, "{name}", repo)
}

// pushURLFor returns the push URL for repo, or "" if the remote has none.
func (r remoteSpec) pushURLFor(repo string) string {
	return strings.ReplaceAll(r.PushURL, "{name}", repo)
}

func (r Remote) validate() error {
	switch r.Kind {
	case RemoteKindPrivate, RemoteKindBackup, RemoteKindPublish:
	default:
		return fmt.Errorf("unknown kind %q (want %s, %s or %s)", r.Kind, RemoteKindPrivate, RemoteKindBackup, RemoteKindPublish)
	}
	if !strings.Contains(r.URL, "{name}") {
		return fmt.Errorf("url %q does not contain {name}", r.URL)
	}
	if r.PushURL != "" && !strings.Contains(r.PushURL, "{name}") {
		return fmt.Errorf("pushurl %q does not contain {name}", r.PushURL)
	}
	return nil
}

// repoSet is the [repositories] configuration merged from all repositories.
type repoSet struct {
	names   []string
	remotes []remoteSpec           // sorted by name
	configs map[string]*Repository // keyed by repository path
}

// mergeRepositories merges the [repositories] sections of repos. Names are
// deduplicated. A remote may be defined in several configs only if all
// definitions are identical.
func mergeRepositories(repos []*Repository) (*repoSet, error) {
	s := &repoSet{configs: make(map[string]*Repository)}
	byName := make(map[string]remoteSpec)
	var errs []error
	for _, r := range repos {
		s.configs[r.srcRoot] = r
		for _, name := range r.config.Repositories.Names {
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
				errs = append(errs, fmt.Errorf("%s: invalid repository name %q", r.configPath, name))
				continue
			}
			s.names = append(s.names, name)
		}
		for _, name := range slices.Sorted(maps.Keys(r.config.Repositories.Remote)) {
			remote := r.config.Repositories.Remote[name]
			if prev, ok := byName[name]; ok {
				if prev.Remote != remote {
					errs = append(errs, fmt.Errorf("remote %q is defined differently in %s and %s", name, prev.source, r.configPath))
				}
				continue
			}
			if err := remote.validate(); err != nil {
				errs = append(errs, fmt.Errorf("%s: remote %q: %w", r.configPath, name, err))
				continue
			}
			byName[name] = remoteSpec{Name: name, Remote: remote, source: r.configPath, repo: r}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	slices.Sort(s.names)
	s.names = slices.Compact(s.names)
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		spec := byName[name]
		spec.active = evalCondition(spec.repo.exprEnv(), spec.source, fmt.Sprintf("Remote(%s)", name), spec.Condition)
		s.remotes = append(s.remotes, spec)
	}
	return s, nil
}

// loadRepoSet loads the config of every repository in d.repos and merges their
// [repositories] sections. Repositories whose config fails to load are
// reported and left out.
func (d *Dotfiles) loadRepoSet() (*repoSet, error) {
	return mergeRepositories(d.loadRepos())
}

// loadRepos loads the config of every repository in d.repos, with the [vars]
// of all of them merged (see mergeVars). Repositories without a config are
// skipped; those whose config fails to load are reported and left out.
func (d *Dotfiles) loadRepos(opts ...Option) []*Repository {
	fail := func(r *Repository, err error) {
		fmt.Fprintf(d.Stderr, "error loading config for %s: %v\n", r.srcRoot, err)
		d.collectErr(fmt.Errorf("config %s: %w", filepath.Base(r.srcRoot), err))
	}
	var read []*Repository
	for _, path := range d.repos {
		r := NewRepository(path, d.DstPath, opts...)
		if err := r.readConfig(); err != nil {
			if !errors.Is(err, ErrConfigMissing) {
				fail(r, err)
			}
			continue
		}
		read = append(read, r)
	}

	views, warnings := mergeVars(read)
	for _, w := range warnings {
		fmt.Fprintf(d.Stderr, "warning: %s\n", w)
	}
	var loaded []*Repository
	for _, r := range read {
		if err := r.resolve(views[r]); err != nil {
			fail(r, err)
			continue
		}
		loaded = append(loaded, r)
	}
	return loaded
}

// configured reports whether any remotes are configured. Without them, push,
// pull and publish fall back to the remote names origin, backup.* and publish.
func (s *repoSet) configured() bool { return len(s.remotes) > 0 }

func (s *repoSet) public(path string) bool {
	r, ok := s.configs[path]
	return ok && r.config.Public
}

// active returns the remotes of the given kinds whose condition is true.
func (s *repoSet) active(kinds ...string) []remoteSpec {
	var out []remoteSpec
	for _, r := range s.remotes {
		if r.active && slices.Contains(kinds, r.Kind) {
			out = append(out, r)
		}
	}
	return out
}

// upstream returns the remote to clone and pull from: the active private
// remote or, if there is none, the active publish remote. The latter lets
// repositories be cloned from their public copies without the private config.
func (s *repoSet) upstream() (remoteSpec, error) {
	for _, kind := range []string{RemoteKindPrivate, RemoteKindPublish} {
		rs := s.active(kind)
		switch len(rs) {
		case 0:
			continue
		case 1:
			return rs[0], nil
		}
		var names []string
		for _, r := range rs {
			names = append(names, r.Name)
		}
		return remoteSpec{}, fmt.Errorf("more than one active %s remote: %s", kind, strings.Join(names, ", "))
	}
	return remoteSpec{}, errors.New("no active private or publish remote configured")
}

// publishViolations returns the publish remotes present in remotes when the
// repository at path is not public. Conditions are ignored: a publish remote
// on a private repository is an error on every host.
func (s *repoSet) publishViolations(path string, remotes map[string]gitRemote) []string {
	if s.public(path) {
		return nil
	}
	names := []string{"publish"}
	if s.configured() {
		names = nil
		for _, r := range s.remotes {
			if r.Kind == RemoteKindPublish {
				names = append(names, r.Name)
			}
		}
	}
	var found []string
	for _, name := range names {
		if _, ok := remotes[name]; ok {
			found = append(found, name)
		}
	}
	return found
}

// gitRemote holds the URLs configured for a git remote.
type gitRemote struct {
	url     string
	pushURL string // remote.<name>.pushurl, if set
}

// pushTo returns the URL that git pushes to.
func (g gitRemote) pushTo() string { return cmp.Or(g.pushURL, g.url) }

// gitRemotes returns the remotes of the repository at path, keyed by remote
// name.
func gitRemotes(path string) (map[string]gitRemote, error) {
	cmd := exec.Command("git", "config", "--get-regexp", `^remote\..*\.(url|pushurl)$`)
	cmd.Dir = path
	out, err := cmd.Output()
	remotes := make(map[string]gitRemote)
	if err != nil {
		// Exit status 1 means that no remote has a URL.
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
			return remotes, nil
		}
		return nil, err
	}
	for line := range strings.Lines(string(out)) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		key = strings.TrimPrefix(key, "remote.")
		if name, ok := strings.CutSuffix(key, ".pushurl"); ok {
			r := remotes[name]
			r.pushURL = value
			remotes[name] = r
		} else if name, ok := strings.CutSuffix(key, ".url"); ok {
			r := remotes[name]
			r.url = value
			remotes[name] = r
		}
	}
	return remotes, nil
}

type backupState int

const (
	backupAvailable   backupState = iota
	backupNotCreated              // the parent directory exists but the repository does not
	backupUnavailable             // the parent directory is missing, e.g. the disk is not mounted
)

// localPath returns the filesystem path of a remote URL that refers to a local
// repository.
func localPath(url string) (string, bool) {
	if p, ok := strings.CutPrefix(url, "file://"); ok {
		return p, true
	}
	if filepath.IsAbs(url) {
		return url, true
	}
	return "", false
}

// localBackupState checks a backup remote URL that refers to a local path.
// Other URLs are assumed to be available.
func localBackupState(url string) backupState {
	p, ok := localPath(url)
	if !ok || isDir(p) {
		return backupAvailable
	}
	if isDir(filepath.Dir(p)) {
		return backupNotCreated
	}
	return backupUnavailable
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
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

func (d *Dotfiles) fail(err error) {
	fmt.Fprintf(d.Stderr, "error: %v\n", err)
	d.collectErr(err)
}

// Repos runs a repos subcommand.
func (d *Dotfiles) Repos(args []string) error {
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
			d.ReposFix()
		} else {
			d.ReposStatus()
		}
	case "clone":
		d.ReposClone(args[1:])
	default:
		return fmt.Errorf("unknown repos command: %s", sub)
	}
	return nil
}

// ReposStatus prints a table of every known repository and the state of each
// configured remote.
func (d *Dotfiles) ReposStatus() {
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

// ReposFix adds missing remotes and corrects remote URLs in every cloned
// repository listed in repositories.names, and creates missing local backup
// repositories. It never removes remotes.
func (d *Dotfiles) ReposFix() {
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
		return &ExecCommandAction{SrcRoot: e.path, Command: "git", Args: args}
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

// ReposClone clones the repositories in repositories.names that are missing
// from the repositories path, or only those in names if any are given, from
// the upstream remote. It then fixes the remotes of every listed repository.
func (d *Dotfiles) ReposClone(names []string) {
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

	var cloned []string
	for _, e := range d.repoEntries(set) {
		if e.cloned || !e.listed || (len(names) > 0 && !slices.Contains(names, e.name)) {
			continue
		}
		d.runActions([]Action{&GitCloneAction{Remote: upstream.Name, URL: upstream.urlFor(e.name), Dst: e.path}})
		if isDir(e.path) {
			cloned = append(cloned, e.path)
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
