package selfupdate

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Run is the body of the `update` subcommand. backend-platform's CLI dispatch
// calls it as: selfupdate.Run(ctx, os.Args[2:], version), where version is the
// running binary's main.version (build-stamped via -ldflags).
//
// Flags:
//
//	--check          report the latest version without applying anything
//	--unit <name>    systemd unit to restart after replace (default: auto-detect)
//	--no-restart     replace the binary but do not restart any unit
//
// It returns a non-nil error on failure; callers map that to a non-zero exit.
func Run(ctx context.Context, args []string, version string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "report the latest version without applying it")
	unit := fs.String("unit", "", "systemd unit to restart after update (default: auto-detect)")
	noRestart := fs.Bool("no-restart", false, "replace the binary but do not restart any unit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedUnit := *unit
	if resolvedUnit == "" && !*noRestart {
		resolvedUnit = DetectUnit()
	}
	if *noRestart {
		resolvedUnit = ""
	}

	u := New(Options{
		CurrentVersion: version,
		Unit:           resolvedUnit,
	})

	if *check {
		latest, newer, err := u.Check(ctx)
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}
		if newer {
			fmt.Printf("update available: %s (current: %s)\n", latest, displayVersion(version))
		} else {
			fmt.Printf("up to date: %s\n", latest)
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	// Follow symlinks so we replace the real file, not a /usr/local/bin symlink.
	if resolved, lerr := filepath.EvalSymlinks(exe); lerr == nil {
		exe = resolved
	}

	applied, err := u.Update(ctx, exe)
	switch {
	case errors.Is(err, ErrUpToDate):
		fmt.Printf("already up to date: %s\n", applied)
		return nil
	case err != nil:
		return err
	}
	fmt.Printf("updated to %s; binary at %s\n", applied, exe)
	if resolvedUnit == "" {
		fmt.Println("no systemd unit restarted; restart the process to run the new version")
	}
	return nil
}

func displayVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}
