package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kallioli/swisstransfert-cli/internal/config"
	"github.com/kallioli/swisstransfert-cli/internal/progress"
	"github.com/kallioli/swisstransfert-cli/internal/swisstransfer"
)

const sendUsage = `Usage: swisstransfer send [options] PATH...

Send files or folders. Folders are sent recursively, keeping their structure.
With an API token (see 'swisstransfer login') the transfer is linked to your Infomaniak account.
Without a token, use --from: a confirmation code is e-mailed to that address.

Options:
  --to EMAIL            Recipient (repeatable or comma-separated). Switches to e-mail mode.
  --from EMAIL          Anonymous send: sender address, confirmed by an e-mailed code
  --code CODE           Confirmation code (otherwise prompted)
  --title TEXT          Transfer title
  --message TEXT        Message shown to recipients
  --password P          Download password
  --password-stdin      Read the download password from stdin (first line)
  --days N              Validity in days (default 7; allowed values come from the server)
  --max-downloads N     Maximum number of downloads, 1-250 (default 250)
  --lang CODE           Language of e-mails/pages: fr, en, de, it, es, nl, pt (default en)
  --parallel N          Chunks uploaded in parallel (default 4, max 16)
  --dry-run             Validate on the server without creating a transfer

Output: the download URL on stdout; progress on stderr. --json prints a JSON object.
`

var validLangs = map[string]bool{"fr": true, "en": true, "de": true, "it": true, "es": true, "nl": true, "pt": true}

func (a *app) cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, sendUsage) }
	a.addGlobalFlags(fs)
	var to stringList
	fs.Var(&to, "to", "")
	from := fs.String("from", "", "")
	code := fs.String("code", "", "")
	title := fs.String("title", "", "")
	message := fs.String("message", "", "")
	password := fs.String("password", "", "")
	passwordStdin := fs.Bool("password-stdin", false, "")
	days := fs.Int("days", 0, "")
	maxDL := fs.Int("max-downloads", 0, "")
	lang := fs.String("lang", "", "")
	parallel := fs.Int("parallel", 0, "")
	dryRun := fs.Bool("dry-run", false, "")
	paths, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return usagef("send: at least one file or folder is required\n\n%s", sendUsage)
	}

	// Defaults from config.
	if *days == 0 {
		*days = a.cfg.Days
	}
	if *days == 0 {
		*days = 7
	}
	if *maxDL == 0 {
		*maxDL = a.cfg.MaxDownloads
	}
	if *maxDL == 0 {
		*maxDL = 250
	}
	if *maxDL < 1 || *maxDL > 250 {
		return usagef("send: --max-downloads must be between 1 and 250")
	}
	if *parallel == 0 {
		*parallel = a.cfg.Parallel
	}
	if *parallel == 0 {
		*parallel = 4
	}
	if *parallel < 1 || *parallel > 16 {
		return usagef("send: --parallel must be between 1 and 16")
	}
	if *lang == "" {
		*lang = a.language()
	}
	if !validLangs[*lang] {
		return usagef("send: unsupported language %q (fr, en, de, it, es, nl, pt)", *lang)
	}
	for _, r := range to {
		if !strings.Contains(r, "@") {
			return usagef("send: invalid recipient %q", r)
		}
	}
	if *from != "" && !strings.Contains(*from, "@") {
		return usagef("send: invalid sender address %q", *from)
	}
	pw, err := a.passwordFrom(*password, *passwordStdin)
	if err != nil {
		return err
	}

	files, total, err := collectFiles(paths)
	if err != nil {
		return err
	}

	anonymous := *from != "" || a.token() == ""
	if anonymous && *from == "" {
		return &swisstransfer.Error{Kind: swisstransfer.KindAuth,
			Message: "no API token configured",
			Hint:    "run `swisstransfer login`, or send anonymously with `--from you@example.com`"}
	}

	ctx, stop := signalContext()
	defer stop()

	base := swisstransfer.SendBase
	if anonymous {
		base = swisstransfer.DownloadBase
	}
	client, err := a.newClient(base, !anonymous)
	if err != nil {
		return err
	}
	a.infof("Preparing %d file(s), %s", len(files), progress.Bytes(total))
	if _, err := client.Bootstrap(ctx); err != nil {
		return err
	}
	if lim := client.Limits(); lim != nil && len(lim.ExpiryDays) > 0 && !containsInt(lim.ExpiryDays, *days) {
		return usagef("send: --days %d is not allowed for this account (allowed: %s)", *days, joinInts(lim.ExpiryDays))
	}

	req := swisstransfer.CreateRequest{
		Method:        "link",
		Title:         *title,
		Recipients:    []string(to),
		Message:       *message,
		Password:      pw,
		Language:      *lang,
		MaxDownload:   *maxDL,
		ExpiresInDays: *days,
	}
	if len(to) > 0 {
		req.Method = "email"
	}
	for _, f := range files {
		req.Files = append(req.Files, swisstransfer.FileDeclaration{Path: f.Name, Size: f.Size, MimeType: f.MimeType})
	}

	if anonymous {
		req.Email = *from
		id, err := a.ensureEmailValidation(ctx, client, *from, *code)
		if err != nil {
			return err
		}
		req.EmailValidationID = id
	}

	if *dryRun {
		if err := client.Validate(ctx, req); err != nil {
			return err
		}
		a.infof("Dry run: the server accepted the transfer (%d file(s), %s). Nothing was sent.", len(files), progress.Bytes(total))
		if a.jsonOut {
			fmt.Fprintln(a.stdout, `{"dry_run":true,"ok":true}`)
		}
		return nil
	}

	a.infof("Creating transfer…")
	created, err := client.Create(ctx, req)
	if err != nil {
		return err
	}
	tid := created.Transfer.ID
	a.logf("transfer %s created", tid)

	showProgress := !a.quiet && !a.jsonOut
	stderrFile, _ := a.stderr.(*os.File)
	tty := stderrFile != nil && progress.IsTerminal(stderrFile)
	bar := progress.New(a.stderr, tty, showProgress, total)

	uploadErr := func() error {
		for _, f := range files {
			fid, ok := created.FileID(f.Name)
			if !ok {
				return fmt.Errorf("server did not register file %q", f.Name)
			}
			if err := client.UploadFile(ctx, tid, fid, f, *parallel, bar); err != nil {
				return err
			}
			bar.FileDone(f.Name, f.Size)
		}
		return nil
	}()
	bar.Finish()
	if uploadErr != nil {
		a.infof("Upload failed, cancelling transfer…")
		if cerr := client.Cancel(tid); cerr != nil {
			a.logf("cancel failed: %v", cerr)
		}
		if ctx.Err() != nil {
			return context.Canceled
		}
		return uploadErr
	}

	link, err := client.Complete(context.Background(), tid)
	if err != nil {
		return err
	}
	if a.jsonOut {
		out := map[string]any{
			"download_url": link.DownloadURL,
			"link_id":      link.ID,
			"transfer_id":  tid,
			"expires_at":   created.Transfer.Expiry().UTC().Format(time.RFC3339),
			"total_size":   created.Transfer.TotalSize,
			"files":        created.Transfer.Files,
		}
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	a.infof("Expires %s", created.Transfer.Expiry().Local().Format("2006-01-02 15:04"))
	fmt.Fprintln(a.stdout, link.DownloadURL)
	return nil
}

// ensureEmailValidation returns a confirmed validation id for an anonymous sender address.
// A pending or confirmed id is cached in the config so that a code e-mailed during a previous
// run can be supplied with --code, and so that a confirmed address is not asked again.
func (a *app) ensureEmailValidation(ctx context.Context, client *swisstransfer.Client, email, code string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if a.cfg.EmailValidations == nil {
		a.cfg.EmailValidations = map[string]string{}
	}
	save := func() {
		if err := config.Save(a.configPath, a.cfg); err != nil {
			a.logf("could not cache email validation: %v", err)
		}
	}
	id := a.cfg.EmailValidations[email]
	if id != "" {
		ok, err := client.EmailValidated(ctx, id, email)
		switch {
		case err == nil && ok:
			a.logf("reusing validated sender address %s", email)
			return id, nil
		case err == nil && code != "":
			a.logf("pending validation %s, trying the supplied code", id)
		default:
			if err != nil {
				a.logf("cached validation %s unusable: %v", id, err)
			}
			id = ""
		}
	}
	if id == "" {
		v, err := client.RequestEmailValidation(ctx, email)
		if err != nil {
			return "", err
		}
		id = v.ID
		a.cfg.EmailValidations[email] = id
		save()
		a.infof("A confirmation code was sent to %s (valid until %s).", email, v.Expiry().Local().Format("15:04"))
	}
	attempts := 3
	for i := 0; i < attempts; i++ {
		c := code
		if c == "" {
			if !a.isInteractive() {
				return "", &swisstransfer.Error{Kind: swisstransfer.KindPassword,
					Message: "a confirmation code is required", Hint: "check your mailbox and re-run the same command with --code XXXXXX"}
			}
			s, err := a.prompt("Enter the 6-character code: ")
			if err != nil {
				return "", err
			}
			c = strings.ToUpper(strings.TrimSpace(s))
		}
		err := client.ConfirmEmailValidation(ctx, id, c)
		if err == nil {
			return id, nil
		}
		var se *swisstransfer.Error
		if errors.As(err, &se) && se.Code == "invalid_validation_token" {
			if code == "" && i < attempts-1 {
				a.infof("Invalid code, try again.")
				continue
			}
			return "", &swisstransfer.Error{Kind: swisstransfer.KindPassword, Code: se.Code, Message: "invalid confirmation code",
				Hint: "codes expire after a few minutes; re-run without --code to request a new one"}
		}
		if errors.As(err, &se) && (se.Code == "email_validation_request_locked" || se.Code == "email_validation_rate_limit_error") {
			delete(a.cfg.EmailValidations, email)
			save()
		}
		return "", err
	}
	return "", &swisstransfer.Error{Kind: swisstransfer.KindPassword, Message: "too many invalid codes"}
}

// collectFiles expands files and folders into LocalFile entries with '/'-separated relative names.
func collectFiles(paths []string) ([]swisstransfer.LocalFile, int64, error) {
	var out []swisstransfer.LocalFile
	seen := map[string]string{}
	var total int64
	add := func(local, name string, size int64) error {
		if prev, dup := seen[name]; dup {
			return fmt.Errorf("duplicate relative path %q (%s and %s)", name, prev, local)
		}
		seen[name] = local
		out = append(out, swisstransfer.LocalFile{Path: local, Name: name, Size: size, MimeType: mimeOf(name)})
		total += size
		return nil
	}
	for _, p := range paths {
		p = filepath.Clean(p)
		st, err := os.Stat(p)
		if err != nil {
			return nil, 0, err
		}
		if !st.IsDir() {
			if !st.Mode().IsRegular() {
				return nil, 0, fmt.Errorf("%s: not a regular file", p)
			}
			if err := add(p, filepath.Base(p), st.Size()); err != nil {
				return nil, 0, err
			}
			continue
		}
		root := p
		parent := filepath.Dir(root)
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil // symlinks are not followed
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(parent, path)
			if err != nil {
				return err
			}
			return add(path, filepath.ToSlash(rel), info.Size())
		})
		if err != nil {
			return nil, 0, err
		}
	}
	if len(out) == 0 {
		return nil, 0, errors.New("no files to send (empty folder?)")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, total, nil
}

func mimeOf(name string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		if i := strings.Index(t, ";"); i > 0 {
			t = t[:i]
		}
		return t
	}
	return "application/octet-stream"
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func joinInts(list []int) string {
	s := make([]string, len(list))
	for i, v := range list {
		s[i] = fmt.Sprint(v)
	}
	return strings.Join(s, ", ")
}
