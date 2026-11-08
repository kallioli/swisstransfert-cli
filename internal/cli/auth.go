package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/kallioli/swisstransfert-cli/internal/config"
	"github.com/kallioli/swisstransfert-cli/internal/swisstransfer"
)

const loginUsage = `Usage: swisstransfer login [--token T] [--space-id N] [--no-verify]

Store an Infomaniak API token in the config file (mode 0600). Create one in the Infomaniak
manager: Developer → API tokens. kSuite accounts also need their space id (the number in
ksuite.infomaniak.com/<id>/...). The token is verified against SwissTransfer unless --no-verify.
`

func (a *app) cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, loginUsage) }
	a.addGlobalFlags(fs)
	noVerify := fs.Bool("no-verify", false, "")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	token := a.flagToken
	if token == "" {
		if !a.isInteractive() {
			return usagef("login: --token is required when not running interactively")
		}
		s, err := a.promptSecret("Infomaniak API token: ")
		if err != nil {
			return err
		}
		token = strings.TrimSpace(s)
	}
	if token == "" {
		return usagef("login: empty token")
	}
	space := a.flagSpace
	if space == "" {
		space = a.cfg.SpaceID
	}
	if !*noVerify {
		ctx, stop := signalContext()
		defer stop()
		client, err := swisstransfer.New(swisstransfer.Options{Base: swisstransfer.SendBase, Token: token, SpaceID: space, UserAgent: "swisstransfer-cli/" + Version, Log: a.logf})
		if err != nil {
			return err
		}
		_, err = client.Bootstrap(ctx)
		var se *swisstransfer.Error
		if errors.As(err, &se) && se.Kind == swisstransfer.KindAuth && strings.Contains(se.Message, "space id") && a.isInteractive() && space == "" {
			s, perr := a.prompt("This is a kSuite account. Space id: ")
			if perr != nil {
				return perr
			}
			space = strings.TrimSpace(s)
			client, _ = swisstransfer.New(swisstransfer.Options{Base: swisstransfer.SendBase, Token: token, SpaceID: space, UserAgent: "swisstransfer-cli/" + Version, Log: a.logf})
			_, err = client.Bootstrap(ctx)
		}
		if err != nil {
			return err
		}
		if u := client.User(); u != nil {
			a.infof("Logged in as %s %s <%s>", u.Firstname, u.Lastname, u.Email)
		} else {
			a.infof("Token accepted.")
		}
	}
	a.cfg.Token = token
	a.cfg.SpaceID = space
	if err := config.Save(a.configPath, a.cfg); err != nil {
		return err
	}
	a.infof("Saved to %s", a.configPath)
	return nil
}

func (a *app) cmdLogout(args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	a.addGlobalFlags(fs)
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	a.cfg.Token = ""
	a.cfg.SpaceID = ""
	if err := config.Save(a.configPath, a.cfg); err != nil {
		return err
	}
	a.infof("Credentials removed from %s", a.configPath)
	return nil
}

const configUsage = `Usage: swisstransfer config <path|show|set KEY VALUE|unset KEY>

Keys: token, space_id, language, days, max_downloads, parallel
`

func (a *app) cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, configUsage) }
	a.addGlobalFlags(fs)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	if len(pos) == 0 {
		return usagef("config: a subcommand is required\n\n%s", configUsage)
	}
	switch pos[0] {
	case "path":
		fmt.Fprintln(a.stdout, a.configPath)
		return nil
	case "show":
		shown := *a.cfg
		shown.Token = config.MaskToken(shown.Token)
		if a.jsonOut {
			return writeJSON(a.stdout, shown)
		}
		fmt.Fprintf(a.stdout, "# %s\n", a.configPath)
		for _, k := range config.Keys {
			v, _ := shown.Get(k)
			if v != "" {
				fmt.Fprintf(a.stdout, "%s = %s\n", k, v)
			}
		}
		if len(shown.EmailValidations) > 0 {
			fmt.Fprintf(a.stdout, "email_validations = %d cached address(es)\n", len(shown.EmailValidations))
		}
		return nil
	case "set":
		if len(pos) != 3 {
			return usagef("config set: KEY and VALUE are required")
		}
		if err := a.cfg.Set(pos[1], pos[2]); err != nil {
			return usagef("config set: %v", err)
		}
		return config.Save(a.configPath, a.cfg)
	case "unset":
		if len(pos) != 2 {
			return usagef("config unset: KEY is required")
		}
		if err := a.cfg.Set(pos[1], ""); err != nil {
			return usagef("config unset: %v", err)
		}
		return config.Save(a.configPath, a.cfg)
	}
	return usagef("config: unknown subcommand %q\n\n%s", pos[0], configUsage)
}
