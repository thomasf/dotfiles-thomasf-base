package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// LogCommand prints the commits of all repositories as one log, ordered by
// commit date with the newest first. When stdout is a terminal, the log is
// colored and shown in git's pager.
func (d *Dotfiles) LogCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles log", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles log [options]\n\n")
		fmt.Fprintf(d.Stderr, "Show the commits of all repositories as one log, newest first, like git log --stat.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}
	var compact, short, patch, textconv bool
	var limit int
	fs.BoolVar(&compact, "compact", false, "show one line per commit, with its date, repository, hash, author email and subject")
	fs.BoolVar(&short, "short", false, "like -compact, with the number of changed files and lines under each commit")
	fs.BoolVar(&patch, "patch", false, "show the patch of each commit")
	fs.BoolVar(&textconv, "textconv", false, "run textconv filters, such as gpg decryption, on the patches")
	fs.IntVar(&limit, "n", 0, "show at most `n` commits; 0 shows all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("log: unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if compact && short {
		fs.Usage()
		return errors.New("log: -compact and -short cannot be combined")
	}

	opts := logOptions{
		format: defaultLogFormat,
		args:   []string{"--stat"},
		color:  isTerminal(d.Stdout),
	}
	if compact || short {
		opts.format = compactLogFormat
		opts.args = nil
		if short {
			opts.args = []string{"--shortstat"}
		}
	}
	if patch {
		opts.args = append(opts.args, "--patch")
	}
	if !textconv {
		// Showing patches would otherwise, for example, decrypt files with
		// gpg.
		opts.args = append(opts.args, "--no-textconv")
	}
	if limit > 0 {
		// No repository can contribute more than limit commits.
		opts.args = append(opts.args, "-n", strconv.Itoa(limit))
	}
	for _, repoPath := range d.repos {
		opts.width = max(opts.width, len(filepath.Base(repoPath)))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var streams []*logStream
	for _, repoPath := range d.repos {
		s, err := startLogStream(ctx, repoPath, opts)
		if err != nil {
			d.fail(err)
			continue
		}
		streams = append(streams, s)
	}

	out, closePager := d.startPager(opts.color)
	var (
		prev    *logStream
		shown   int
		stopped bool // the remaining commits are not wanted
	)
	for {
		var next *logStream
		for _, s := range streams {
			if s.ok && (next == nil || s.time > next.time) {
				next = s
			}
		}
		if next == nil {
			break
		}
		if limit > 0 && shown == limit {
			stopped = true
			break
		}
		entry := next.entry
		if prev != nil && prev.format.separated {
			entry = append([]byte("\n"), entry...)
		}
		if _, err := out.Write(entry); err != nil {
			// The pager has quit, so nobody reads the rest.
			stopped = true
			break
		}
		shown++
		prev = next
		next.advance()
	}
	if stopped {
		cancel()
	}
	closePager()

	for _, s := range streams {
		// Stopping kills the git processes that are still running, even if
		// they have printed everything.
		if err := s.wait(); err != nil && !(stopped && (s.ok || errors.Is(err, context.Canceled))) {
			d.fail(err)
		}
	}
	return nil
}

// logFormat is a git log format string.
type logFormat struct {
	format    string // {name} is replaced by the repository name
	separated bool   // used with "format:" rather than "tformat:"
	oneline   bool   // no blank line or --- between the first line and the diff
}

// defaultLogFormat is the format of LogCommand.
var defaultLogFormat = logFormat{
	format:    "%C(green)%ad%C(reset) {name} %C(yellow)%H%C(reset) %C(cyan)%ae%C(reset)%n%n%s%n%n%b",
	separated: true,
}

// compactLogFormat is the format of LogCommand -compact and -short.
var compactLogFormat = logFormat{
	format:  "%C(green)%ad%C(reset) {name} %C(yellow)%h%C(reset) %C(cyan)%ae%C(reset) %s",
	oneline: true,
}

// logOptions are the options of LogCommand for every repository.
type logOptions struct {
	format logFormat
	args   []string // for git log
	color  bool
	width  int // of the repository names
}

// logMarker starts every commit in the output of git log, followed by the
// commit time and logMarkerEnd.
const (
	logMarker    = "\x1edotfiles-log "
	logMarkerEnd = '\x1f'
)

// logStream reads the log of one repository.
type logStream struct {
	name     string
	format   logFormat
	cmd      *exec.Cmd
	stdout   *bufio.Reader
	stderr   bytes.Buffer
	atMarker bool // stdout is positioned after a logMarker

	ok    bool  // time and entry hold the next commit
	time  int64 // commit time, in seconds since the epoch
	entry []byte
	err   error // a malformed time
}

func startLogStream(ctx context.Context, repoPath string, opts logOptions) (*logStream, error) {
	s := &logStream{name: filepath.Base(repoPath), format: opts.format}
	name := "%C(magenta)" + strings.ReplaceAll(fmt.Sprintf("%-*s", opts.width, s.name), "%", "%%") + "%C(reset)"
	format := "%x1edotfiles-log %ct%x1f" + strings.Replace(opts.format.format, "{name}", name, 1)
	if opts.format.separated {
		format = "format:" + format
	} else {
		format = "tformat:" + format
	}
	args := append([]string{"log", "--format=" + format}, opts.args...)
	if opts.color {
		args = append(args, "--color=always")
	}

	s.cmd = exec.CommandContext(ctx, "git", args...)
	s.cmd.Dir = repoPath
	s.cmd.Stderr = &s.stderr
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", s.name, err)
	}
	s.stdout = bufio.NewReader(stdout)
	_, s.atMarker = s.readEntry()
	s.advance()
	return s, nil
}

// readEntry reads up to and including the next logMarker. It reports whether
// it found one.
func (s *logStream) readEntry() ([]byte, bool) {
	var entry []byte
	for {
		b, err := s.stdout.ReadBytes(logMarker[0])
		if err != nil {
			return append(entry, b...), false
		}
		if next, _ := s.stdout.Peek(len(logMarker) - 1); string(next) == logMarker[1:] {
			s.stdout.Discard(len(next))
			return append(entry, b[:len(b)-1]...), true
		}
		entry = append(entry, b...)
	}
}

// advance reads the next commit.
func (s *logStream) advance() {
	s.ok = false
	if !s.atMarker {
		// The end of the log, or git failed, which wait reports.
		return
	}
	t, err := s.stdout.ReadString(logMarkerEnd)
	if err != nil {
		return
	}
	s.time, err = strconv.ParseInt(strings.TrimSuffix(t, string(logMarkerEnd)), 10, 64)
	if err != nil {
		s.err = fmt.Errorf("%s: git log: unexpected commit time %q", s.name, t)
		return
	}
	s.entry, s.atMarker = s.readEntry()
	if s.format.separated && s.atMarker {
		// LogCommand writes the separator only between the commits it prints.
		s.entry = bytes.TrimSuffix(s.entry, []byte("\n"))
	}
	if first, rest, ok := bytes.Cut(s.entry, []byte("\n")); ok && s.format.oneline {
		// git puts these between a tformat: line and the diff, but not
		// between the line of the built-in oneline format and the diff.
		rest = bytes.TrimPrefix(rest, []byte("\n"))
		rest = bytes.TrimPrefix(rest, []byte("---\n"))
		s.entry = slices.Concat(first, []byte("\n"), rest)
	}
	s.ok = true
}

// wait waits for git to exit and returns its error.
func (s *logStream) wait() error {
	// Drain the output, so git does not block writing what was not read.
	io.Copy(io.Discard, s.stdout)
	if err := s.cmd.Wait(); err != nil {
		return cmp.Or(s.err, fmt.Errorf("%s: git log: %w: %s", s.name, err, strings.TrimSpace(s.stderr.String())))
	}
	return s.err
}

// startPager starts git's pager with its output going to d.Stdout, if paging
// is wanted, and returns the writer to print to. close waits for the pager to
// exit.
func (d *Dotfiles) startPager(paging bool) (w io.Writer, close func()) {
	noPager := func() {}
	if !paging {
		return d.Stdout, noPager
	}
	out, err := exec.Command("git", "var", "GIT_PAGER").Output()
	pager := strings.TrimSpace(string(out))
	if err != nil || pager == "" || pager == "cat" {
		return d.Stdout, noPager
	}

	// Git for Windows does not put its sh on PATH.
	sh, err := exec.LookPath("sh")
	if err != nil {
		return d.Stdout, noPager
	}
	cmd := exec.Command(sh, "-c", pager)
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	// The defaults git uses for less and lv.
	cmd.Env = os.Environ()
	if _, ok := os.LookupEnv("LESS"); !ok {
		cmd.Env = append(cmd.Env, "LESS=FRX")
	}
	if _, ok := os.LookupEnv("LV"); !ok {
		cmd.Env = append(cmd.Env, "LV=-c")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return d.Stdout, noPager
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(d.Stderr, "warning: could not start pager %q: %v\n", pager, err)
		return d.Stdout, noPager
	}
	return stdin, func() {
		stdin.Close()
		cmd.Wait()
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
