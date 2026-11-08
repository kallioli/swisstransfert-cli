# swisstransfer-cli

Unofficial command-line client for [SwissTransfer](https://www.swisstransfer.com) (Infomaniak).
Send and download transfers from a terminal on Linux, macOS and Windows (PowerShell), with a
single static binary and no dependencies.

> **Disclaimer.** SwissTransfer has no public API. This tool speaks the protocol of the web
> application, reverse-engineered in November 2026. It may stop working at any time if Infomaniak
> changes the app. It is not affiliated with or endorsed by Infomaniak.

## Features

- `send` files **and folders** (structure preserved), as a link or by e-mail, with optional
  password, validity (1–30 days) and download limit.
- Works with an **Infomaniak API token** (transfers linked to your account, kSuite supported)
  or **anonymously** with an e-mailed confirmation code, exactly like the website.
- `download` / `info` any transfer link, password protected or not. No account needed.
- Chunked parallel uploads (50 MiB chunks), retries, resumable-friendly S3 presigned URLs.
- Scriptable: results on stdout, progress on stderr, `--json`, documented exit codes.
- Shell completion for bash, zsh, fish and PowerShell.

## Install

Download the binary for your platform from the
[releases page](https://github.com/kallioli/swisstransfert-cli/releases), put it in your `PATH`
and rename it `swisstransfer` (`swisstransfer.exe` on Windows).

```sh
# macOS / Linux
chmod +x swisstransfer && sudo mv swisstransfer /usr/local/bin/
# PowerShell
Move-Item .\swisstransfer.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\swisstransfer.exe"
```

Or build from source (Go ≥ 1.22, no other dependency):

```sh
go install github.com/kallioli/swisstransfert-cli/cmd/swisstransfer@latest
```

## Quick start

```sh
# Download a transfer (no account needed)
swisstransfer download https://www.swisstransfer.com/dl/01a1bb9a-763c-728b-9984-b114dfb634fd
swisstransfer info  https://www.swisstransfer.com/dl/01a1bb9a-...   --password secret

# Send anonymously: a 6-character code is e-mailed to you, type it when prompted
swisstransfer send --from you@example.com report.pdf photos/

# Send with your Infomaniak account (API token from manager.infomaniak.com → Developer → API tokens)
swisstransfer login
swisstransfer send --days 30 --password secret --title "Q4 files" report.pdf photos/
swisstransfer send --to alice@example.com,bob@example.com --message "Here you go" big.iso
```

`send` prints the download URL on stdout, so it composes well:

```sh
URL=$(swisstransfer send -q archive.zip) && echo "Get it here: $URL" | mail -s "Files" alice@example.com
```

## Commands

```
swisstransfer send [options] PATH...        Send files/folders
swisstransfer download [options] LINK       Download a transfer (alias: get)
swisstransfer info [options] LINK           List the files of a transfer
swisstransfer login | logout                Store / remove the API token
swisstransfer config path|show|set|unset    Manage the config file
swisstransfer completion SHELL              bash, zsh, fish, powershell
swisstransfer version
```

### send

| Option | Description |
|---|---|
| `--to EMAIL` | Recipient (repeatable or comma-separated). Switches to e-mail mode. |
| `--from EMAIL` | Anonymous send: your address, confirmed by an e-mailed code |
| `--code XXXXXX` | Confirmation code (otherwise prompted; needed in scripts) |
| `--title`, `--message` | Title and message |
| `--password P`, `--password-stdin` | Download password |
| `--days N` | Validity: 1, 3, 7 (default), 15, 30 (allowed values depend on your plan) |
| `--max-downloads N` | 1–250 (default 250) |
| `--lang fr\|en\|de\|it\|es\|nl\|pt` | Language of notification e-mails (default `en`) |
| `--parallel N` | Chunks uploaded in parallel (default 4) |
| `--dry-run` | Validate on the server, send nothing |

Folders are sent recursively; the relative path (`folder/sub/file.ext`) is kept so recipients see
the same structure. Symbolic links are skipped. Press Ctrl-C to abort: the transfer is cancelled
on the server.

Anonymous sends: the first time, the server e-mails a code. Interactively the CLI asks for it; in
a script, run the command once (it exits with code 5 and tells you a code was sent), then run the
exact same command again with `--code XXXXXX`. A confirmed address is cached in the config file, so
later sends need no code until the server forgets it.

### download / info

| Option | Description |
|---|---|
| `-o, --output DIR` | Destination folder (default: current) |
| `--file NAME` | Only this file (repeatable; exact path or glob such as `*.pdf`) |
| `--password P`, `--password-stdin` | Password (or `SWISSTRANSFER_PASSWORD`; prompted on a terminal) |
| `--force` | Overwrite existing files (default: rename to `name (1).ext`) |
| `--parallel N` | Files downloaded in parallel (default 2) |

Files are written through a `.part` temporary file and renamed once their size is verified.
`download` prints one local path per file on stdout; `--json` prints a summary object.

### Global options

`--json`, `-q/--quiet`, `-v/--verbose` (HTTP log, token masked), `--config PATH`, `--token T`,
`--space-id N`. They may be placed before or after the command.

## Configuration

Precedence: command-line flag > environment variable > config file > default.

| Config key | Environment variable | Meaning |
|---|---|---|
| `token` | `SWISSTRANSFER_TOKEN` (or `IK_API_TOKEN`) | Infomaniak API token |
| `space_id` | `SWISSTRANSFER_SPACE_ID` | kSuite space id (required for kSuite accounts) |
| `language` | `SWISSTRANSFER_LANG` | default `en` |
| `days`, `max_downloads`, `parallel` | — | defaults for `send` |
| — | `SWISSTRANSFER_PASSWORD` | password for `send`/`download`/`info` |
| — | `SWISSTRANSFER_CONFIG` | config file path |

Config file (JSON, mode 0600): `~/.config/swisstransfer/config.json` on Linux/macOS
(`$XDG_CONFIG_HOME` honoured), `%APPDATA%\swisstransfer\config.json` on Windows.
`swisstransfer config path` prints the exact location.

kSuite accounts: the space id is the number in your kSuite URL (`ksuite.infomaniak.com/<id>/…`).
`swisstransfer login` asks for it when the server requires one.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic error (API, network, I/O) |
| 2 | usage error |
| 3 | authentication (no/invalid token, space id required) |
| 4 | transfer not found or expired |
| 5 | password or confirmation code required / wrong |
| 130 | interrupted (Ctrl-C) |

## Shell completion

```sh
source <(swisstransfer completion bash)                                  # bash
swisstransfer completion zsh > "${fpath[1]}/_swisstransfer"              # zsh, then restart
swisstransfer completion fish > ~/.config/fish/completions/swisstransfer.fish
swisstransfer completion powershell | Out-String | Invoke-Expression     # PowerShell (add to $PROFILE)
```

## Building

```sh
make build          # bin/swisstransfer for the current platform
make dist           # all six targets + archives + SHA256SUMS in dist/
./scripts/build.sh  # same without make (POSIX sh); scripts/build.ps1 for PowerShell
```

Targets: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64.
Tagging `vX.Y.Z` triggers the GitHub release workflow.

## How it works

See [SPECS.md](SPECS.md) (French) for the full specification and protocol notes. In short:
session bootstrap on the web app, Altcha proof-of-work captcha solved locally, Inertia.js JSON
page requests, S3 multipart upload through presigned URLs, and the public download endpoints.

## License

MIT — see [LICENSE](LICENSE).
