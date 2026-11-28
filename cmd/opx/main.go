// Command opx signs in to 1Password once per session and picks item fields
// with rofi.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
)

const usage = `usage: opx [-p profile] <command>

commands:
  signin   print an export line for the 1Password session, signing in if needed
  rofi     pick an item and field with rofi and copy or type it
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "opx:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fs := flag.NewFlagSet("opx", flag.ContinueOnError)
	profileName := fs.String("p", "", "profile in ~/.config/opx/profiles/ (default: default_profile in ~/.config/opx/config.toml)")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage, "\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.Arg(0) == "clipboard-restore" {
		return restoreClipboard(fs.Arg(1))
	}

	dir, err := defaultConfigDir()
	if err != nil {
		return err
	}
	profile, err := loadProfile(dir, *profileName)
	if err != nil {
		return err
	}

	switch cmd := fs.Arg(0); cmd {
	case "signin":
		return signin(ctx, profile)
	case "rofi":
		err := pick(ctx, profile)
		if errors.Is(err, errCancelled) {
			return nil
		}
		if err != nil {
			notify(true, err.Error())
		}
		return err
	case "":
		fs.Usage()
		return flag.ErrHelp
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func signin(ctx context.Context, profile Profile) error {
	s, err := newSessions(profile)
	if err != nil {
		return err
	}
	v, err := s.get(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Print(v.export())
	return err
}
