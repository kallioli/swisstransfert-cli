package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/kallioli/swisstransfert-cli/internal/progress"
	"github.com/kallioli/swisstransfert-cli/internal/swisstransfer"
)

const downloadUsage = `Usage: swisstransfer download [options] LINK

Download the files of a transfer. LINK is a SwissTransfer URL or a bare transfer id.
No authentication is needed.

Options:
  -o, --output DIR      Destination folder (default: current folder)
  --file NAME           Only download this file (repeatable; exact path or glob pattern)
  --password P          Transfer password
  --password-stdin      Read the password from stdin (first line)
  --force               Overwrite existing files (default: rename to 'name (1).ext')
  --parallel N          Files downloaded in parallel (default 2, max 8)

Output: one local path per downloaded file on stdout. --json prints a summary object.
`

const infoUsage = `Usage: swisstransfer info [options] LINK

Show the title, expiry and files of a transfer without downloading it.

Options:
  --password P          Transfer password
  --password-stdin      Read the password from stdin (first line)
`

// openTransfer fetches the transfer behind a link, unlocking it with a password when needed.
func (a *app) openTransfer(ctx context.Context, link, password string, passwordStdin bool) (*swisstransfer.Client, string, *swisstransfer.Transfer, error) {
	linkID, err := swisstransfer.ParseLink(link)
	if err != nil {
		return nil, "", nil, usagef("%v", err)
	}
	client, err := a.newClient(swisstransfer.DownloadBase, false)
	if err != nil {
		return nil, "", nil, err
	}
	t, protected, err := client.Open(ctx, linkID)
	if err != nil {
		return nil, "", nil, err
	}
	if !protected {
		return client, linkID, t, nil
	}
	pw, err := a.passwordFrom(password, passwordStdin)
	if err != nil {
		return nil, "", nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		if pw == "" {
			if !a.isInteractive() {
				return nil, "", nil, &swisstransfer.Error{Kind: swisstransfer.KindPassword,
					Message: "this transfer is password protected", Hint: "use --password, --password-stdin or SWISSTRANSFER_PASSWORD"}
			}
			pw, err = a.promptSecret("Password: ")
			if err != nil {
				return nil, "", nil, err
			}
		}
		t, err = client.Unlock(ctx, linkID, pw)
		if err == nil {
			return client, linkID, t, nil
		}
		var se *swisstransfer.Error
		if errors.As(err, &se) && se.Kind == swisstransfer.KindPassword && password == "" && !passwordStdin && a.isInteractive() && attempt < 2 {
			a.infof("Wrong password, try again.")
			pw = ""
			continue
		}
		return nil, "", nil, err
	}
	return nil, "", nil, &swisstransfer.Error{Kind: swisstransfer.KindPassword, Message: "wrong password"}
}

func (a *app) cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, infoUsage) }
	a.addGlobalFlags(fs)
	password := fs.String("password", "", "")
	passwordStdin := fs.Bool("password-stdin", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("info: exactly one LINK is required\n\n%s", infoUsage)
	}
	ctx, stop := signalContext()
	defer stop()
	_, linkID, t, err := a.openTransfer(ctx, pos[0], *password, *passwordStdin)
	if err != nil {
		return err
	}
	if a.jsonOut {
		return writeJSON(a.stdout, transferJSON(linkID, t, nil))
	}
	title := ""
	if t.Title != nil {
		title = *t.Title
	}
	w := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	if title != "" {
		fmt.Fprintf(w, "Title:\t%s\n", title)
	}
	if t.Message != nil && *t.Message != "" {
		fmt.Fprintf(w, "Message:\t%s\n", strings.ReplaceAll(*t.Message, "\n", " "))
	}
	exp := t.Expiry()
	fmt.Fprintf(w, "Expires:\t%s (%s)\n", exp.Local().Format("2006-01-02 15:04"), humanUntil(exp))
	fmt.Fprintf(w, "Files:\t%d, %s\n", len(t.Files), progress.Bytes(t.TotalSize))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "SIZE\tTYPE\tPATH\n")
	for _, f := range t.Files {
		fmt.Fprintf(w, "%s\t%s\t%s\n", progress.Bytes(f.Size), f.MimeType, f.Path)
	}
	return w.Flush()
}

func humanUntil(t time.Time) string {
	d := time.Until(t)
	switch {
	case d <= 0:
		return "expired"
	case d < time.Hour:
		return fmt.Sprintf("in %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %d h", int(d.Hours()))
	default:
		return fmt.Sprintf("in %d days", int(d.Hours()/24))
	}
}

type fileJSON struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	LocalPath string `json:"local_path,omitempty"`
}

func transferJSON(linkID string, t *swisstransfer.Transfer, local map[string]string) map[string]any {
	files := make([]fileJSON, 0, len(t.Files))
	for _, f := range t.Files {
		files = append(files, fileJSON{ID: f.ID, Path: f.Path, Size: f.Size, MimeType: f.MimeType, LocalPath: local[f.ID]})
	}
	title := ""
	if t.Title != nil {
		title = *t.Title
	}
	return map[string]any{
		"link_id":     linkID,
		"transfer_id": t.ID,
		"title":       title,
		"expires_at":  t.Expiry().UTC().Format(time.RFC3339),
		"total_size":  t.TotalSize,
		"files":       files,
	}
}

func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *app) cmdDownload(args []string) error {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, downloadUsage) }
	a.addGlobalFlags(fs)
	var outDir string
	fs.StringVar(&outDir, "output", "", "")
	fs.StringVar(&outDir, "o", "", "")
	var only stringList
	fs.Var(&only, "file", "")
	password := fs.String("password", "", "")
	passwordStdin := fs.Bool("password-stdin", false, "")
	force := fs.Bool("force", false, "")
	parallel := fs.Int("parallel", 2, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("download: exactly one LINK is required\n\n%s", downloadUsage)
	}
	if *parallel < 1 || *parallel > 8 {
		return usagef("download: --parallel must be between 1 and 8")
	}
	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()
	client, linkID, t, err := a.openTransfer(ctx, pos[0], *password, *passwordStdin)
	if err != nil {
		return err
	}

	var selected []swisstransfer.File
	var total int64
	for _, f := range t.Files {
		if len(only) > 0 && !matchesAny(f.Path, only) {
			continue
		}
		selected = append(selected, f)
		total += f.Size
	}
	if len(selected) == 0 {
		return fmt.Errorf("no file matches --file %s (transfer has %d file(s), see `swisstransfer info`)", strings.Join(only, ","), len(t.Files))
	}
	a.infof("Downloading %d file(s), %s", len(selected), progress.Bytes(total))

	showProgress := !a.quiet && !a.jsonOut
	stderrFile, _ := a.stderr.(*os.File)
	tty := stderrFile != nil && progress.IsTerminal(stderrFile)
	bar := progress.New(a.stderr, tty, showProgress, total)

	local := map[string]string{}
	var mu sync.Mutex
	sem := make(chan struct{}, *parallel)
	var wg sync.WaitGroup
	var firstErr error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, f := range selected {
		f := f
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			dest, err := a.downloadOne(ctx, client, linkID, f, outDir, *force, bar)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			local[f.ID] = dest
			bar.FileDone(f.Path, f.Size)
		}()
	}
	wg.Wait()
	bar.Finish()
	if firstErr != nil {
		if ctx.Err() != nil && errors.Is(firstErr, context.Canceled) {
			return context.Canceled
		}
		return firstErr
	}
	if a.jsonOut {
		out := transferJSON(linkID, t, local)
		out["output_dir"] = outDir
		return writeJSON(a.stdout, out)
	}
	paths := make([]string, 0, len(local))
	for _, p := range local {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintln(a.stdout, p)
	}
	return nil
}

func matchesAny(p string, patterns []string) bool {
	base := path.Base(p)
	for _, pat := range patterns {
		if pat == p || pat == base {
			return true
		}
		if ok, _ := path.Match(pat, p); ok {
			return true
		}
		if ok, _ := path.Match(pat, base); ok {
			return true
		}
	}
	return false
}

// downloadOne streams a file to outDir/<path> via a temporary .part file and returns the final path.
func (a *app) downloadOne(ctx context.Context, client *swisstransfer.Client, linkID string, f swisstransfer.File, outDir string, force bool, bar *progress.Bar) (string, error) {
	rel := safeRelPath(f.Path)
	dest := filepath.Join(outDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if !force {
		dest = uniquePath(dest)
	}
	u, err := client.FileURL(ctx, linkID, f.ID)
	if err != nil {
		return "", err
	}
	tmp := dest + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	bar.SetLabel(f.Path)
	n, err := client.Fetch(ctx, u, out, bar)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("%s: %w", f.Path, err)
	}
	if n != f.Size {
		os.Remove(tmp)
		return "", fmt.Errorf("%s: size mismatch (got %d bytes, expected %d)", f.Path, n, f.Size)
	}
	if force {
		os.Remove(dest)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

// safeRelPath neutralises path traversal in server-provided names.
func safeRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	var clean []string
	for _, s := range parts {
		if s == "" || s == "." || s == ".." {
			continue
		}
		clean = append(clean, s)
	}
	if len(clean) == 0 {
		return "unnamed"
	}
	return strings.Join(clean, "/")
}

// uniquePath returns p, or "name (n).ext" when p already exists.
func uniquePath(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for i := 1; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
	}
	return p
}
