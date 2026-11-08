package cli

import (
	"flag"
	"fmt"
	"strings"
)

const completionUsage = `Usage: swisstransfer completion bash|zsh|fish|powershell

Print a shell completion script. Installation:

  bash:        source <(swisstransfer completion bash)
               # or: swisstransfer completion bash > /etc/bash_completion.d/swisstransfer
  zsh:         swisstransfer completion zsh > "${fpath[1]}/_swisstransfer"   (then restart zsh)
  fish:        swisstransfer completion fish > ~/.config/fish/completions/swisstransfer.fish
  PowerShell:  swisstransfer completion powershell | Out-String | Invoke-Expression
               # persist: add that line to $PROFILE
`

var globalFlagNames = []string{"--json", "--quiet", "--verbose", "--config", "--token", "--space-id", "--help"}

// commandFlags lists the per-command flags offered by completion.
var commandFlags = map[string][]string{
	"send":       {"--to", "--from", "--code", "--title", "--message", "--password", "--password-stdin", "--days", "--max-downloads", "--lang", "--parallel", "--dry-run"},
	"download":   {"--output", "-o", "--file", "--password", "--password-stdin", "--force", "--parallel"},
	"get":        {"--output", "-o", "--file", "--password", "--password-stdin", "--force", "--parallel"},
	"info":       {"--password", "--password-stdin"},
	"login":      {"--no-verify"},
	"logout":     {},
	"config":     {},
	"completion": {},
	"version":    {},
}

var configSubcommands = []string{"path", "show", "set", "unset"}

func commandNames() []string {
	var names []string
	for _, c := range commands {
		names = append(names, c.name)
		names = append(names, c.aliases...)
	}
	return append(names, "help")
}

func flagsFor(cmd string) []string {
	return append(append([]string{}, commandFlags[cmd]...), globalFlagNames...)
}

func (a *app) cmdCompletion(args []string) error {
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, completionUsage) }
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("completion: one shell is required\n\n%s", completionUsage)
	}
	var script string
	switch pos[0] {
	case "bash":
		script = bashCompletion()
	case "zsh":
		script = zshCompletion()
	case "fish":
		script = fishCompletion()
	case "powershell", "pwsh":
		script = powershellCompletion()
	default:
		return usagef("completion: unsupported shell %q (bash, zsh, fish, powershell)", pos[0])
	}
	fmt.Fprint(a.stdout, script)
	return nil
}

func bashCompletion() string {
	var b strings.Builder
	b.WriteString("# bash completion for swisstransfer\n_swisstransfer() {\n  local cur prev cmd i\n  COMPREPLY=()\n  cur=\"${COMP_WORDS[COMP_CWORD]}\"\n  prev=\"${COMP_WORDS[COMP_CWORD-1]}\"\n  cmd=\"\"\n  for ((i=1; i<COMP_CWORD; i++)); do\n    case \"${COMP_WORDS[i]}\" in\n      -*) ;;\n      *) cmd=\"${COMP_WORDS[i]}\"; break ;;\n    esac\n  done\n")
	b.WriteString("  if [[ -z \"$cmd\" ]]; then\n    COMPREPLY=( $(compgen -W \"" + strings.Join(commandNames(), " ") + " " + strings.Join(globalFlagNames, " ") + "\" -- \"$cur\") )\n    return\n  fi\n")
	b.WriteString("  case \"$prev\" in\n    --config|--output|-o) compopt -o filenames 2>/dev/null; COMPREPLY=( $(compgen -f -- \"$cur\") ); return ;;\n    --lang) COMPREPLY=( $(compgen -W \"fr en de it es nl pt\" -- \"$cur\") ); return ;;\n    --days) COMPREPLY=( $(compgen -W \"1 3 7 15 30\" -- \"$cur\") ); return ;;\n    --to|--from|--code|--title|--message|--password|--token|--space-id|--max-downloads|--parallel|--file) return ;;\n  esac\n")
	b.WriteString("  case \"$cmd\" in\n")
	for _, c := range commands {
		names := append([]string{c.name}, c.aliases...)
		fmt.Fprintf(&b, "    %s)\n", strings.Join(names, "|"))
		if c.name == "completion" {
			b.WriteString("      COMPREPLY=( $(compgen -W \"bash zsh fish powershell\" -- \"$cur\") ) ;;\n")
			continue
		}
		if c.name == "config" {
			b.WriteString("      if [[ \"$cur\" == -* ]]; then COMPREPLY=( $(compgen -W \"" + strings.Join(globalFlagNames, " ") + "\" -- \"$cur\") ); else COMPREPLY=( $(compgen -W \"" + strings.Join(configSubcommands, " ") + " " + strings.Join(configKeyList(), " ") + "\" -- \"$cur\") ); fi ;;\n")
			continue
		}
		fmt.Fprintf(&b, "      if [[ \"$cur\" == -* ]]; then COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") ); ", strings.Join(flagsFor(c.name), " "))
		if c.name == "send" {
			b.WriteString("else compopt -o filenames 2>/dev/null; COMPREPLY=( $(compgen -f -- \"$cur\") ); fi ;;\n")
		} else {
			b.WriteString("fi ;;\n")
		}
	}
	b.WriteString("  esac\n}\ncomplete -F _swisstransfer swisstransfer\n")
	return b.String()
}

func configKeyList() []string {
	return []string{"token", "space_id", "language", "days", "max_downloads", "parallel"}
}

func zshCompletion() string {
	var b strings.Builder
	b.WriteString("#compdef swisstransfer\n# zsh completion for swisstransfer\n_swisstransfer() {\n  local -a cmds\n  cmds=(\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "    '%s:%s'\n", c.name, strings.ReplaceAll(c.summary, "'", ""))
		for _, al := range c.aliases {
			fmt.Fprintf(&b, "    '%s:alias for %s'\n", al, c.name)
		}
	}
	b.WriteString("    'help:Show help'\n  )\n  local -a gflags\n  gflags=(\n    '--json[JSON output]' '(-q --quiet)'{-q,--quiet}'[no progress]' '(-v --verbose)'{-v,--verbose}'[log HTTP requests]'\n    '--config[config file]:file:_files' '--token[API token]:token' '--space-id[kSuite space id]:id' '--help[show help]'\n  )\n")
	b.WriteString("  if (( CURRENT == 2 )); then\n    _describe -t commands 'command' cmds\n    _arguments $gflags\n    return\n  fi\n  local cmd=${words[2]}\n  case $cmd in\n")
	b.WriteString("    send)\n      _arguments $gflags \\\n        '*--to[recipient]:email' '--from[anonymous sender]:email' '--code[confirmation code]:code' '--title[title]:text' '--message[message]:text' \\\n        '--password[password]:password' '--password-stdin[read password from stdin]' '--days[validity]:days:(1 3 7 15 30)' '--max-downloads[max downloads]:n' \\\n        '--lang[language]:lang:(fr en de it es nl pt)' '--parallel[parallel chunks]:n' '--dry-run[validate only]' '*:file:_files' ;;\n")
	b.WriteString("    download|get)\n      _arguments $gflags \\\n        '(-o --output)'{-o,--output}'[destination folder]:dir:_files -/' '*--file[only this file]:name' '--password[password]:password' '--password-stdin[read password from stdin]' \\\n        '--force[overwrite existing files]' '--parallel[parallel files]:n' ':link' ;;\n")
	b.WriteString("    info)\n      _arguments $gflags '--password[password]:password' '--password-stdin[read password from stdin]' ':link' ;;\n")
	b.WriteString("    login)\n      _arguments $gflags '--no-verify[do not verify the token]' ;;\n")
	b.WriteString("    config)\n      if (( CURRENT == 3 )); then _values 'subcommand' path show set unset; elif (( CURRENT == 4 )); then _values 'key' " + strings.Join(configKeyList(), " ") + "; fi ;;\n")
	b.WriteString("    completion)\n      _values 'shell' bash zsh fish powershell ;;\n")
	b.WriteString("    *) _arguments $gflags ;;\n  esac\n}\n_swisstransfer \"$@\"\n")
	return b.String()
}

func fishCompletion() string {
	var b strings.Builder
	b.WriteString("# fish completion for swisstransfer\ncomplete -c swisstransfer -f\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "complete -c swisstransfer -n __fish_use_subcommand -a %s -d '%s'\n", c.name, strings.ReplaceAll(c.summary, "'", ""))
		for _, al := range c.aliases {
			fmt.Fprintf(&b, "complete -c swisstransfer -n __fish_use_subcommand -a %s -d 'alias for %s'\n", al, c.name)
		}
	}
	b.WriteString("complete -c swisstransfer -n __fish_use_subcommand -a help -d 'Show help'\n")
	b.WriteString("complete -c swisstransfer -l json -d 'JSON output'\ncomplete -c swisstransfer -s q -l quiet -d 'No progress'\ncomplete -c swisstransfer -s v -l verbose -d 'Log HTTP requests'\ncomplete -c swisstransfer -l config -r -F -d 'Config file'\ncomplete -c swisstransfer -l token -r -d 'API token'\ncomplete -c swisstransfer -l space-id -r -d 'kSuite space id'\ncomplete -c swisstransfer -l help -d 'Show help'\n")
	b.WriteString("complete -c swisstransfer -n '__fish_seen_subcommand_from send' -F\n")
	send := []struct{ f, d string }{{"to", "Recipient"}, {"from", "Anonymous sender"}, {"code", "Confirmation code"}, {"title", "Title"}, {"message", "Message"}, {"password", "Password"}, {"days", "Validity in days"}, {"max-downloads", "Max downloads"}, {"lang", "Language"}, {"parallel", "Parallel chunks"}}
	for _, s := range send {
		fmt.Fprintf(&b, "complete -c swisstransfer -n '__fish_seen_subcommand_from send' -l %s -r -d '%s'\n", s.f, s.d)
	}
	b.WriteString("complete -c swisstransfer -n '__fish_seen_subcommand_from send' -l password-stdin -d 'Read password from stdin'\ncomplete -c swisstransfer -n '__fish_seen_subcommand_from send' -l dry-run -d 'Validate only'\n")
	b.WriteString("complete -c swisstransfer -n '__fish_seen_subcommand_from download get' -s o -l output -r -F -d 'Destination folder'\ncomplete -c swisstransfer -n '__fish_seen_subcommand_from download get' -l file -r -d 'Only this file'\ncomplete -c swisstransfer -n '__fish_seen_subcommand_from download get info' -l password -r -d 'Password'\ncomplete -c swisstransfer -n '__fish_seen_subcommand_from download get info' -l password-stdin -d 'Read password from stdin'\ncomplete -c swisstransfer -n '__fish_seen_subcommand_from download get' -l force -d 'Overwrite existing files'\ncomplete -c swisstransfer -n '__fish_seen_subcommand_from download get' -l parallel -r -d 'Parallel files'\n")
	b.WriteString("complete -c swisstransfer -n '__fish_seen_subcommand_from login' -l no-verify -d 'Do not verify the token'\n")
	b.WriteString("complete -c swisstransfer -n '__fish_seen_subcommand_from config' -a 'path show set unset'\n")
	b.WriteString("complete -c swisstransfer -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish powershell'\n")
	return b.String()
}

func powershellCompletion() string {
	var b strings.Builder
	b.WriteString("# PowerShell completion for swisstransfer\nRegister-ArgumentCompleter -Native -CommandName swisstransfer, swisstransfer.exe -ScriptBlock {\n  param($wordToComplete, $commandAst, $cursorPosition)\n  $elements = $commandAst.CommandElements | Select-Object -Skip 1 | ForEach-Object { $_.ToString() }\n  $cmd = $null\n  foreach ($e in $elements) { if ($e -notlike '-*' -and $e -ne $wordToComplete) { $cmd = $e; break } }\n")
	b.WriteString("  $commands = @{\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "    '%s' = '%s'\n", c.name, strings.ReplaceAll(c.summary, "'", "''"))
		for _, al := range c.aliases {
			fmt.Fprintf(&b, "    '%s' = 'Alias for %s'\n", al, c.name)
		}
	}
	b.WriteString("    'help' = 'Show help'\n  }\n")
	b.WriteString("  $globalFlags = @(" + psList(globalFlagNames) + ")\n  $flags = @{\n")
	for name, fl := range commandFlags {
		fmt.Fprintf(&b, "    '%s' = @(%s)\n", name, psList(fl))
	}
	b.WriteString("  }\n  $values = @{ '--lang' = @('fr','en','de','it','es','nl','pt'); '--days' = @('1','3','7','15','30') }\n")
	b.WriteString("  $prev = if ($elements.Count -ge 2) { $elements[$elements.Count - 2] } else { $null }\n  if ($wordToComplete -eq '' -and $elements.Count -ge 1) { $prev = $elements[$elements.Count - 1] }\n")
	b.WriteString("  if ($prev -and $values.ContainsKey($prev)) {\n    $values[$prev] | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }\n    return\n  }\n")
	b.WriteString("  if (-not $cmd) {\n    $commands.Keys | Sort-Object | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'Command', $commands[$_]) }\n    return\n  }\n")
	b.WriteString("  if ($cmd -eq 'completion') { @('bash','zsh','fish','powershell') | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }; return }\n")
	b.WriteString("  if ($cmd -eq 'config') { @('path','show','set','unset'," + psList(configKeyList()) + ") | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }; return }\n")
	b.WriteString("  if ($wordToComplete -like '-*') {\n    $list = @(); if ($flags.ContainsKey($cmd)) { $list += $flags[$cmd] }; $list += $globalFlags\n    $list | Sort-Object -Unique | Where-Object { $_ -like \"$wordToComplete*\" } | ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterName', $_) }\n  }\n}\n")
	return b.String()
}

func psList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + s + "'"
	}
	return strings.Join(q, ",")
}
