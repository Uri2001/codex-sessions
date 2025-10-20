package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/Uri2001/codex-sessions/internal/sessions"
	"github.com/Uri2001/codex-sessions/internal/ui"
)

var (
	flagSessionsDir = flag.String("sessions-dir", "", "Path to the Codex CLI sessions directory. Defaults to ~/.codex/sessions.")
	flagCodexBin    = flag.String("codex-bin", "codex", "Codex CLI binary to invoke for resuming sessions.")
	flagNoResume    = flag.Bool("no-resume", false, "Do not automatically run `codex resume`. Print the selected ID instead.")
)

func main() {
	flag.Parse()

	root, err := sessions.ResolveDir(*flagSessionsDir)
	if err != nil {
		fatalf("resolve sessions dir: %v", err)
	}

	list, loadErr := sessions.Load(root)
	var status string
	if loadErr != nil {
		status = loadErr.Error()
		fmt.Fprintf(os.Stderr, "warning: %v\n", loadErr)
	}

	selectedID, err := ui.Run(list, root, status)
	if err != nil {
		fatalf("run ui: %v", err)
	}
	if selectedID == "" {
		return
	}

	if *flagNoResume {
		fmt.Println(selectedID)
		return
	}

	if err := runCodexResume(selectedID, *flagCodexBin, flag.Args()); err != nil {
		fatalf("codex resume %s: %v", selectedID, err)
	}
}

func runCodexResume(sessionID, codexBin string, extraArgs []string) error {
	args := append([]string{"resume", sessionID}, extraArgs...)
	cmd := exec.Command(codexBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("command exited with status %d", exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
