package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

	theme := d.selectTheme(read)
	for _, r := range read {
		r.theme = theme
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

// selectTheme loads the themes of repos and returns the theme variant in use
// (see selectTheme), or nil if there is none. Problems are reported; a theme
// that fails to load is left out, and if the selected one is not available,
// there is no theme.
func (d *Dotfiles) selectTheme(repos []*Repository) *activeTheme {
	ts, warnings, err := mergeThemes(repos)
	for _, w := range warnings {
		fmt.Fprintf(d.Stderr, "warning: %s\n", w)
	}
	if err != nil {
		fmt.Fprintf(d.Stderr, "error loading themes: %v\n", err)
		d.collectErr(fmt.Errorf("themes: %w", err))
	}
	theme, err := selectTheme(ts, d.Theme, d.DstPath)
	if err != nil {
		fmt.Fprintf(d.Stderr, "error selecting the theme: %v\n", err)
		d.collectErr(fmt.Errorf("theme: %w", err))
	}
	return theme
}
