package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WhichCommand reports the source or destination path of dotfile targets.
func (d *Dotfiles) WhichCommand(args []string) error {
	fs := flag.NewFlagSet("dotfiles which", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	verbose := fs.Bool("v", false, "verbose output with repository and action details")
	fs.Usage = func() {
		fmt.Fprintf(d.Stderr, "Usage: dotfiles which [options] <path...>\n\n")
		fmt.Fprintf(d.Stderr, "Print the source or destination path of dotfile targets.\n\n")
		fmt.Fprintf(d.Stderr, "Resolves destination paths in the target directory (such as ~/.bashrc) to\n")
		fmt.Fprintf(d.Stderr, "their source files in dotfiles repositories, and source files in repositories\n")
		fmt.Fprintf(d.Stderr, "to their destination paths. Paths inside mounted directories are also\n")
		fmt.Fprintf(d.Stderr, "resolved to their corresponding source file.\n\n")
		fmt.Fprintf(d.Stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fs.Usage()
		return fmt.Errorf("which: at least one path argument required")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not get home directory: %w", err)
	}

	// Gather all actions from loaded repos
	var allActions []Action
	for _, r := range d.loadRepos(allParts, WithForce(d.Force), WithCopy(d.Copy), WithEvent(eventInstall)) {
		actions, err := r.Sync()
		if err != nil {
			continue
		}
		allActions = append(allActions, actions...)
		allActions = append(allActions, r.GoInstall()...)
	}

	var notFound []string
	for _, p := range paths {
		target := p
		if strings.HasPrefix(target, "~/") || target == "~" {
			target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
		}
		absTarget, err := filepath.Abs(target)
		if err != nil {
			absTarget = target
		}
		realTarget := realPath(absTarget)

		matched := false
		for _, a := range allActions {
			var src, dst string
			if s, ok := a.(Sourcer); ok {
				src = s.SrcAbsolutePath()
			}
			if t, ok := a.(Targeter); ok {
				dst = t.DstAbsolutePath()
			}

			// If target matches dst (or is inside dst directory)
			if dst != "" {
				if absTarget == dst || realTarget == realPath(dst) {
					if *verbose {
						fmt.Fprintf(d.Stdout, "%s -> %s (%s)\n", dst, src, a.String())
					} else {
						fmt.Fprintln(d.Stdout, src)
					}
					matched = true
					break
				}
				if within(absTarget, dst) {
					rel, _ := filepath.Rel(dst, absTarget)
					matchedSrc := filepath.Join(src, rel)
					if *verbose {
						fmt.Fprintf(d.Stdout, "%s -> %s (%s)\n", absTarget, matchedSrc, a.String())
					} else {
						fmt.Fprintln(d.Stdout, matchedSrc)
					}
					matched = true
					break
				}
			}

			// If target matches src (or is inside src directory)
			if src != "" {
				if absTarget == src || realTarget == realPath(src) {
					if *verbose {
						fmt.Fprintf(d.Stdout, "%s -> %s (%s)\n", src, dst, a.String())
					} else {
						fmt.Fprintln(d.Stdout, dst)
					}
					matched = true
					break
				}
				if within(absTarget, src) {
					rel, _ := filepath.Rel(src, absTarget)
					matchedDst := filepath.Join(dst, rel)
					if *verbose {
						fmt.Fprintf(d.Stdout, "%s -> %s (%s)\n", absTarget, matchedDst, a.String())
					} else {
						fmt.Fprintln(d.Stdout, matchedDst)
					}
					matched = true
					break
				}
			}

			// Match by basename if a simple name was passed
			if !strings.ContainsAny(p, "/"+string(filepath.Separator)) {
				if dst != "" && filepath.Base(dst) == p {
					if *verbose {
						fmt.Fprintf(d.Stdout, "%s -> %s (%s)\n", dst, src, a.String())
					} else {
						fmt.Fprintln(d.Stdout, src)
					}
					matched = true
					break
				}
				if src != "" && filepath.Base(src) == p {
					if *verbose {
						fmt.Fprintf(d.Stdout, "%s -> %s (%s)\n", src, dst, a.String())
					} else {
						fmt.Fprintln(d.Stdout, dst)
					}
					matched = true
					break
				}
			}
		}

		if !matched {
			notFound = append(notFound, p)
		}
	}

	if len(notFound) > 0 {
		for _, nf := range notFound {
			fmt.Fprintf(d.Stderr, "which: %s: not managed by dotfiles\n", nf)
			d.collectErr(fmt.Errorf("which: %s: not managed by dotfiles", nf))
		}
		return fmt.Errorf("some paths not found")
	}

	return nil
}
