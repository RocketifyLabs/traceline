package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInBrowser hands a generated report to the platform's default handler.
//
// The CLI does this itself instead of printing "open this file yourself" because
// the report is the product: an artifact nobody opens is an artifact nobody reads.
// It is opt-in (--open) so a CI run or a script never pops a window.
//
// Each platform gets its native launcher. `cmd /c start` needs the empty title
// argument: without it, a path containing spaces is taken as the window title and
// nothing opens.
func openInBrowser(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	// The launcher exits on its own; reap it so no zombie is left behind.
	go func() { _ = cmd.Wait() }()
	return nil
}
