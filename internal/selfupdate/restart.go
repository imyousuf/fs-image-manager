package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// SystemctlRestarter restarts a systemd unit by invoking `systemctl restart`.
// It is the default Restarter. Tests substitute a fake.
type SystemctlRestarter struct{}

// Restart runs `systemctl restart <unit>`. The replaced binary is picked up on
// restart because systemd re-executes ExecStart from the path on disk.
func (SystemctlRestarter) Restart(ctx context.Context, unit string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("selfupdate: systemctl not found, restart %s manually: %w", unit, err)
	}
	cmd := exec.CommandContext(ctx, "systemctl", "restart", unit)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl restart %s: %w", unit, err)
	}
	return nil
}

// DetectUnit returns the systemd unit that owns the current process, or "" if
// the process is not managed by systemd. It reads the INVOCATION_ID/JOURNAL
// environment that systemd sets, falling back to the unit name systemd exports.
func DetectUnit() string {
	// systemd sets these for processes it spawns. UNIT is not always present, so
	// prefer an explicit signal and let the caller override via a flag.
	if u := os.Getenv("FSIM_SYSTEMD_UNIT"); u != "" {
		return u
	}
	if os.Getenv("INVOCATION_ID") == "" {
		return "" // not started by systemd
	}
	return os.Getenv("SYSTEMD_UNIT")
}
