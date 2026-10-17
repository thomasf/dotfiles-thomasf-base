package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
)

// defaultHostCheckTimeout is how long checkSSHHosts waits for a host.
const defaultHostCheckTimeout = 2 * time.Minute

// errInterrupted is the error of work that SIGINT or SIGTERM stopped.
var errInterrupted = errors.New("interrupted")

// interruptContext returns a context that is canceled, with the cause
// errInterrupted, when the process gets SIGINT or SIGTERM. It then reports
// the interrupt and restores the default handling, so that a second signal
// kills the process. stop releases the signal handling.
func (d *Dotfiles) interruptContext() (ctx context.Context, stop func()) {
	ctx, cancel := context.WithCancelCause(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigs:
			signal.Stop(sigs)
			d.interrupted.Store(true)
			d.collectErr(errInterrupted)
			fmt.Fprintf(d.Stderr, "\n%v: stopping git; the summary shows where each repository stopped. Interrupt again to quit at once.\n", sig)
			cancel(errInterrupted)
		case <-done:
		}
	}()
	return ctx, func() {
		signal.Stop(sigs)
		close(done)
		cancel(nil)
	}
}

// remoteTarget is a remote of a repository to pull from or push to.
type remoteTarget struct {
	repoPath string
	remote   string
	url      string // that pulling or pushing connects to; empty if the remote is not configured
	backup   bool   // failures are warnings, not errors
}

// newRemoteTarget returns the target for pulling from remote of the repository
// at repoPath, or if push, for pushing to it.
func newRemoteTarget(repoPath, remote string, push bool) remoteTarget {
	args := []string{"remote", "get-url", remote}
	if push {
		args = []string{"remote", "get-url", "--push", remote}
	}
	url, _ := gitText(repoPath, args...)
	return remoteTarget{repoPath: repoPath, remote: remote, url: url}
}

// remoteResult is the outcome of pulling from or pushing to a remoteTarget.
type remoteResult struct {
	target   remoteTarget
	err      error  // the failure; with skip, the failure that prevented trying
	skip     string // why it was not tried
	note     string // such as "new branch"
	commits  int    // brought in or sent
	stat     string // git diff --shortstat of the commits
	unpushed int    // local commits that the remote lacks, after a pull
}

// runRemotes runs f for every target, prints a summary of the results, along
// with decided, the results known without running anything, and reports
// whether none of them failed. It first checks the ssh hosts of the targets
// and skips the targets whose host fails. Then it runs the targets of each
// repository one after the other, so that they do not compete for the
// repository's locks, and the repositories in parallel.
//
// SIGINT or SIGTERM stops the running git commands and the targets not
// started yet, and the summary shows where each target stopped.
func (d *Dotfiles) runRemotes(targets []remoteTarget, decided []remoteResult, f func(context.Context, remoteTarget) remoteResult) bool {
	ctx, stop := d.interruptContext()
	defer stop()
	unreachable := d.checkSSHHosts(ctx, targets)

	results := make([]remoteResult, len(targets))
	byRepo := make(map[string][]int) // indexes of the targets to run, by repository
	for i, t := range targets {
		if err, ok := unreachable[t]; ok {
			results[i] = remoteResult{target: t, err: err, skip: err.Error()}
		} else {
			byRepo[t.repoPath] = append(byRepo[t.repoPath], i)
		}
	}

	var wg sync.WaitGroup
	for _, indexes := range byRepo {
		wg.Go(func() {
			for _, i := range indexes {
				if ctx.Err() != nil {
					results[i] = remoteResult{target: targets[i], err: errInterrupted, skip: "not started, interrupted"}
					continue
				}
				results[i] = f(ctx, targets[i])
			}
		})
	}
	wg.Wait()

	results = append(results, decided...)
	slices.SortStableFunc(results, func(a, b remoteResult) int {
		return strings.Compare(filepath.Base(a.target.repoPath), filepath.Base(b.target.repoPath))
	})
	d.printRemoteSummary(results)
	return !slices.ContainsFunc(results, func(r remoteResult) bool { return r.err != nil })
}

// runTargetGit runs git with args in t's repository, like runGit, and records
// a failure as an error or, for a backup, a warning. An interrupt is recorded
// once, by interruptContext.
func (d *Dotfiles) runTargetGit(ctx context.Context, t remoteTarget, args ...string) (string, error) {
	out, err := d.runGit(ctx, t.repoPath, args...)
	if err == nil || errors.Is(err, errInterrupted) {
		return out, err
	}
	desc := gitDesc(t.repoPath, args...)
	if t.backup {
		fmt.Fprintf(d.Stderr, "warning: %s: %v\n", desc, err)
	} else {
		d.collectErr(fmt.Errorf("%s: %w", desc, err))
	}
	return out, err
}

// printRemoteSummary prints one line per result with its repository, remote
// and outcome. On a terminal, failures are red, skips yellow and changes
// green.
func (d *Dotfiles) printRemoteSummary(results []remoteResult) {
	if len(results) == 0 {
		return
	}
	color := isTerminal(d.Stdout)
	fmt.Fprintln(d.Stdout)
	w := tabwriter.NewWriter(d.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range results {
		summary := r.summary()
		if c := r.color(); color && c != "" {
			// The outcome is the last column, so the escapes do not upset
			// the alignment.
			summary = c + summary + "\x1b[0m"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", filepath.Base(r.target.repoPath), r.target.remote, summary)
	}
	w.Flush()
}

// color returns the ANSI color of r's summary, or "" for none.
func (r remoteResult) color() string {
	switch {
	case r.err != nil:
		return "\x1b[31m" // red
	case r.skip != "":
		return "\x1b[33m" // yellow
	case r.note != "" || r.commits > 0:
		return "\x1b[32m" // green
	}
	return ""
}

func (r remoteResult) summary() string {
	switch {
	case r.skip != "":
		return "SKIPPED: " + r.skip
	case r.err != nil:
		return "FAILED: " + r.err.Error()
	}
	var commits string
	if r.commits > 0 {
		commits = plural(r.commits, "commit")
	}
	parts := slices.DeleteFunc([]string{r.note, commits, r.stat}, func(s string) bool { return s == "" })
	if len(parts) == 0 {
		parts = []string{"up to date"}
	}
	if r.unpushed > 0 {
		parts = append(parts, plural(r.unpushed, "local commit")+" not pushed")
	}
	return strings.Join(parts, ", ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// changesBetween returns the number of commits in to that are not in from
// and git diff --shortstat of their changes, from where to and from forked.
// It returns 0 and "" if from or to is empty.
func changesBetween(repoPath, from, to string) (commits int, stat string) {
	if from == "" || to == "" {
		return 0, ""
	}
	commits = countCommits(repoPath, from+".."+to)
	if commits > 0 {
		stat, _ = gitText(repoPath, "diff", "--shortstat", from+"..."+to)
	}
	return commits, stat
}

// countCommits returns the number of commits in the revision range r, or 0 if
// they cannot be counted.
func countCommits(repoPath, r string) int {
	out, _ := gitText(repoPath, "rev-list", "--count", r)
	n, _ := strconv.Atoi(out)
	return n
}

// remotesError records err, the failure to read the remotes of the repository
// at repoPath, and returns its result for the summary.
func (d *Dotfiles) remotesError(repoPath string, err error) remoteResult {
	err = fmt.Errorf("cannot read remotes: %w", err)
	d.collectErr(fmt.Errorf("%s: %w", filepath.Base(repoPath), err))
	return remoteResult{target: remoteTarget{repoPath: repoPath}, err: err}
}

// checkSSHHosts checks, in parallel, that every ssh host that the targets
// connect to can be reached and authenticated against by running git
// ls-remote for one target per host. It returns the targets of the hosts that
// failed, with the error of their host, so that they need not be tried and a
// broken login shows once rather than once per target. A host that does not
// answer within d.hostCheckTimeout fails. A failed host is an error, even if
// all its targets are backups.
//
// With ssh's ControlMaster auto and ControlPersist, the check's connection
// also becomes the master that the targets share, instead of them racing to
// create it and the losers warning "ControlSocket ... already exists,
// disabling multiplexing".
func (d *Dotfiles) checkSSHHosts(ctx context.Context, targets []remoteTarget) map[remoteTarget]error {
	hosts := make(map[sshEndpoint][]remoteTarget)
	for _, t := range targets {
		if e, ok := parseSSHURL(t.url); ok {
			hosts[e] = append(hosts[e], t)
		}
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed = make(map[remoteTarget]error)
	)
	timeout := cmp.Or(d.hostCheckTimeout, defaultHostCheckTimeout)
	for e, targets := range hosts {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			first := targets[0]
			args := []string{"ls-remote", first.url, "HEAD"}
			_, err := d.runGit(ctx, first.repoPath, args...)
			switch {
			case err == nil:
				return
			case errors.Is(err, errInterrupted):
				err = fmt.Errorf("%w while checking %s", errInterrupted, e)
			default:
				if errors.Is(err, context.DeadlineExceeded) {
					err = fmt.Errorf("no answer within %v", timeout)
				}
				err = fmt.Errorf("cannot connect to %s: %w", e, err)
				d.collectErr(fmt.Errorf("%s: %w", gitDesc(first.repoPath, args...), err))
			}
			mu.Lock()
			defer mu.Unlock()
			for _, t := range targets {
				failed[t] = err
			}
		})
	}
	wg.Wait()
	return failed
}

// sshEndpoint is what an ssh remote URL connects to.
type sshEndpoint struct {
	user, host, port string // user and port may be empty
}

func (e sshEndpoint) String() string {
	s := e.host
	if e.user != "" {
		s = e.user + "@" + s
	}
	if e.port != "" {
		s += ":" + e.port
	}
	return s
}

// parseSSHURL returns the endpoint of the ssh remote URL url. It reports
// false if url is not an ssh URL.
func parseSSHURL(url string) (sshEndpoint, bool) {
	if scheme, _, ok := strings.Cut(url, "://"); ok {
		switch scheme {
		case "ssh", "git+ssh", "ssh+git":
		default:
			return sshEndpoint{}, false
		}
		u, err := neturl.Parse(url)
		if err != nil || u.Hostname() == "" {
			return sshEndpoint{}, false
		}
		return sshEndpoint{user: u.User.Username(), host: u.Hostname(), port: u.Port()}, true
	}
	// The scp-like syntax [user@]host:path. Like git, treat it as a local
	// path if a slash comes before the first colon.
	dest, _, ok := strings.Cut(url, ":")
	if !ok || dest == "" || strings.Contains(dest, "/") {
		return sshEndpoint{}, false
	}
	if user, host, ok := strings.Cut(dest, "@"); ok {
		return sshEndpoint{user: user, host: host}, true
	}
	return sshEndpoint{host: dest}, true
}
