package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kallioli/swisstransfert-cli/internal/progress"
)

// isInteractive reports whether both stdin and stderr are terminals.
func (a *app) isInteractive() bool {
	stderr, ok := a.stderr.(*os.File)
	return a.stdin != nil && progress.IsTerminal(a.stdin) && ok && progress.IsTerminal(stderr)
}

// readLine reads one line from stdin (without the trailing newline).
func (a *app) readLine() (string, error) {
	r := bufio.NewReader(a.stdin)
	s, err := r.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// prompt prints a prompt on stderr and reads a line.
func (a *app) prompt(label string) (string, error) {
	fmt.Fprint(a.stderr, label)
	return a.readLine()
}

// promptSecret reads a line with echo disabled when the platform allows it (stty on Unix).
func (a *app) promptSecret(label string) (string, error) {
	fmt.Fprint(a.stderr, label)
	restore := disableEcho(a.stdin)
	s, err := a.readLine()
	restore()
	fmt.Fprintln(a.stderr)
	return s, err
}

func disableEcho(f *os.File) func() {
	if runtime.GOOS == "windows" {
		return func() {}
	}
	run := func(arg string) error {
		cmd := exec.Command("stty", arg)
		cmd.Stdin = f
		return cmd.Run()
	}
	if err := run("-echo"); err != nil {
		return func() {}
	}
	return func() { _ = run("echo") }
}

// passwordFrom resolves a password from --password, --password-stdin or SWISSTRANSFER_PASSWORD.
func (a *app) passwordFrom(flagValue string, fromStdin bool) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if fromStdin {
		s, err := a.readLine()
		if err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return s, nil
	}
	return os.Getenv("SWISSTRANSFER_PASSWORD"), nil
}
