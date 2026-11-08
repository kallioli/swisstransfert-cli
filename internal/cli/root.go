// Package cli implements the swisstransfer command-line interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kallioli/swisstransfert-cli/internal/config"
	"github.com/kallioli/swisstransfert-cli/internal/swisstransfer"
)

// Exit codes (see SPECS.md §6).
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitAuth        = 3
	ExitNotFound    = 4
	ExitPassword    = 5
	ExitInterrupted = 130
)

// Version is injected by the linker (-ldflags "-X ...cli.Version=v1.2.3").
var Version = "dev"

// usageError is a user mistake on the command line.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return &usageError{fmt.Sprintf(format, a...)} }

// app carries global state for one invocation.
type app struct {
	stdout io.Writer
	stderr io.Writer
	stdin  *os.File

	jsonOut bool
	quiet   bool
	verbose bool

	configPath string
	flagToken  string
	flagSpace  string

	cfg *config.Config
}

func (a *app) logf(format string, v ...any) {
	if a.verbose {
		fmt.Fprintf(a.stderr, "[debug] "+format+"\n", v...)
	}
}

func (a *app) infof(format string, v ...any) {
	if !a.quiet {
		fmt.Fprintf(a.stderr, format+"\n", v...)
	}
}

// token resolves the API token: flag > env > config.
func (a *app) token() string {
	if a.flagToken != "" {
		return a.flagToken
	}
	if t := os.Getenv("SWISSTRANSFER_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("IK_API_TOKEN"); t != "" {
		return t
	}
	return a.cfg.Token
}

func (a *app) spaceID() string {
	if a.flagSpace != "" {
		return a.flagSpace
	}
	if s := os.Getenv("SWISSTRANSFER_SPACE_ID"); s != "" {
		return s
	}
	return a.cfg.SpaceID
}

func (a *app) language() string {
	if l := os.Getenv("SWISSTRANSFER_LANG"); l != "" {
		return l
	}
	if a.cfg.Language != "" {
		return a.cfg.Language
	}
	return "en"
}

func (a *app) newClient(base string, withToken bool) (*swisstransfer.Client, error) {
	o := swisstransfer.Options{Base: base, UserAgent: "swisstransfer-cli/" + Version, Log: a.logf}
	if withToken {
		o.Token = a.token()
		o.SpaceID = a.spaceID()
	}
	return swisstransfer.New(o)
}

// addGlobalFlags registers the global flags on a subcommand's flag set.
func (a *app) addGlobalFlags(fs *flag.FlagSet) {
	fs.BoolVar(&a.jsonOut, "json", false, "JSON output on stdout")
	fs.BoolVar(&a.quiet, "quiet", false, "no progress or informational messages")
	fs.BoolVar(&a.quiet, "q", false, "alias for --quiet")
	fs.BoolVar(&a.verbose, "verbose", false, "log HTTP requests on stderr")
	fs.BoolVar(&a.verbose, "v", false, "alias for --verbose")
	fs.StringVar(&a.configPath, "config", "", "config file path")
	fs.StringVar(&a.flagToken, "token", "", "Infomaniak API token")
	fs.StringVar(&a.flagSpace, "space-id", "", "kSuite space id")
}

// parseInterspersed parses flags that may appear before, between or after positional arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional, tail []string
	for i, s := range args {
		if s == "--" {
			tail = args[i+1:]
			args = args[:i]
			break
		}
	}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	return append(positional, tail...), nil
}

// stringList is a repeatable flag that also accepts comma-separated values.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

type command struct {
	name    string
	aliases []string
	summary string
	run     func(a *app, args []string) error
}

var commands []command

func init() {
	commands = []command{
		{"send", nil, "Send files or folders (API token or --from for anonymous sends)", (*app).cmdSend},
		{"download", []string{"get"}, "Download a transfer from its link", (*app).cmdDownload},
		{"info", nil, "Show the files of a transfer", (*app).cmdInfo},
		{"login", nil, "Store the Infomaniak API token (and kSuite space id)", (*app).cmdLogin},
		{"logout", nil, "Remove stored credentials", (*app).cmdLogout},
		{"config", nil, "Show or edit the configuration (path, show, set, unset)", (*app).cmdConfig},
		{"completion", nil, "Print a shell completion script (bash, zsh, fish, powershell)", (*app).cmdCompletion},
		{"version", nil, "Print the version", (*app).cmdVersion},
	}
}

func findCommand(name string) *command {
	for i := range commands {
		c := &commands[i]
		if c.name == name {
			return c
		}
		for _, al := range c.aliases {
			if al == name {
				return c
			}
		}
	}
	return nil
}

func rootUsage(w io.Writer) {
	fmt.Fprintf(w, `swisstransfer %s — SwissTransfer command-line client (unofficial)

Usage:
  swisstransfer <command> [options] [arguments]

Commands:
`, Version)
	for _, c := range commands {
		name := c.name
		if len(c.aliases) > 0 {
			name += " (" + strings.Join(c.aliases, ", ") + ")"
		}
		fmt.Fprintf(w, "  %-18s %s\n", name, c.summary)
	}
	fmt.Fprint(w, `
Global options (accepted by every command):
  --json            JSON output on stdout
  -q, --quiet       No progress or informational messages on stderr
  -v, --verbose     Log HTTP requests on stderr (token masked)
  --config PATH     Config file (default: see 'swisstransfer config path')
  --token T         Infomaniak API token (overrides SWISSTRANSFER_TOKEN and the config)
  --space-id N      kSuite space id (overrides SWISSTRANSFER_SPACE_ID and the config)

Run 'swisstransfer <command> --help' for details. Exit codes: 0 ok, 1 error, 2 usage,
3 authentication, 4 transfer not found/expired, 5 password, 130 interrupted.
`)
}

// Run executes the CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer, stdin *os.File) int {
	a := &app{stdout: stdout, stderr: stderr, stdin: stdin}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		if len(args) > 1 {
			if c := findCommand(args[1]); c != nil {
				return a.exit(c.run(a, []string{"--help"}))
			}
		}
		rootUsage(stdout)
		return ExitOK
	}
	if args[0] == "--version" || args[0] == "-V" {
		args[0] = "version"
	}
	// Global flags may precede the command: move them after it.
	lead, rest := splitLeadingFlags(args)
	if len(rest) == 0 {
		rootUsage(stderr)
		return ExitUsage
	}
	c := findCommand(rest[0])
	if c == nil {
		fmt.Fprintf(stderr, "swisstransfer: unknown command %q\n\n", rest[0])
		rootUsage(stderr)
		return ExitUsage
	}
	return a.exit(c.run(a, append(lead, rest[1:]...)))
}

// globalFlagsWithValue lists global flags that take a separate value argument.
var globalFlagsWithValue = map[string]bool{"--config": true, "--token": true, "--space-id": true, "-config": true, "-token": true, "-space-id": true}

func splitLeadingFlags(args []string) (lead, rest []string) {
	for i := 0; i < len(args); i++ {
		s := args[i]
		if !strings.HasPrefix(s, "-") {
			return lead, args[i:]
		}
		lead = append(lead, s)
		if globalFlagsWithValue[s] && i+1 < len(args) {
			lead = append(lead, args[i+1])
			i++
		}
	}
	return lead, nil
}

// loadConfig must be called by commands after flag parsing.
func (a *app) loadConfig() error {
	if a.configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return fmt.Errorf("locate config: %w", err)
		}
		a.configPath = p
	}
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	a.cfg = cfg
	return nil
}

func (a *app) exit(err error) int {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	code := exitCode(err)
	msg := err.Error()
	if code == ExitInterrupted {
		msg = "interrupted"
	}
	fmt.Fprintf(a.stderr, "swisstransfer: %s\n", msg)
	return code
}

func exitCode(err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		return ExitUsage
	}
	if errors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	var se *swisstransfer.Error
	if errors.As(err, &se) {
		switch se.Kind {
		case swisstransfer.KindAuth:
			return ExitAuth
		case swisstransfer.KindNotFound:
			return ExitNotFound
		case swisstransfer.KindPassword:
			return ExitPassword
		}
	}
	return ExitError
}

// signalContext returns a context cancelled on Ctrl-C / SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func (a *app) cmdVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	a.addGlobalFlags(fs)
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if a.jsonOut {
		fmt.Fprintf(a.stdout, "{\"version\":%q}\n", Version)
		return nil
	}
	fmt.Fprintf(a.stdout, "swisstransfer %s\n", Version)
	return nil
}
