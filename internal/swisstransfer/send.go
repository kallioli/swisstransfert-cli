package swisstransfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

// LocalFile pairs a file on disk with the relative path announced to the server.
type LocalFile struct {
	Path     string // local filesystem path
	Name     string // relative path sent to the server, '/'-separated
	Size     int64
	MimeType string
}

// Created is a transfer freshly created on the server, before upload.
type Created struct {
	Transfer *Transfer
	// byName maps the announced relative path to the server file id.
	byName map[string]string
}

// Validate runs the server-side validation only (Laravel Precognition) without creating anything.
func (c *Client) Validate(ctx context.Context, req CreateRequest) error {
	req.Altcha = ""
	if req.Recipients == nil {
		req.Recipients = []string{}
	}
	lang := req.Language
	if lang == "" {
		lang = "en"
	}
	h := http.Header{"Precognition": []string{"true"}}
	_, err := c.doJSON(ctx, "POST", "/"+lang, c.pageQuery(), req, true, h, nil)
	return err
}

// Create solves the captcha and creates the transfer. It returns the server-side file ids.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*Created, error) {
	if c.version == "" {
		if _, err := c.Bootstrap(ctx); err != nil {
			return nil, err
		}
	}
	altcha, err := c.altcha(ctx)
	if err != nil {
		return nil, err
	}
	req.Altcha = altcha
	if req.Recipients == nil {
		req.Recipients = []string{}
	}
	var page Page
	if _, err := c.doJSON(ctx, "POST", "/", c.pageQuery(), req, true, nil, &page); err != nil {
		return nil, err
	}
	c.absorb(&page)
	if len(page.Props.Errors) > 0 {
		e := &Error{Kind: KindValidation, Status: 422, Message: "the server rejected the transfer", Fields: map[string][]string{}}
		for k, v := range page.Props.Errors {
			switch t := v.(type) {
			case string:
				e.Fields[k] = []string{t}
			case []any:
				for _, s := range t {
					e.Fields[k] = append(e.Fields[k], fmt.Sprint(s))
				}
			default:
				e.Fields[k] = []string{fmt.Sprint(v)}
			}
		}
		return nil, e
	}
	if page.Props.Transfer == nil || page.Props.Transfer.ID == "" {
		return nil, newErr(KindGeneric, 200, "unexpected response to transfer creation (component %q)", page.Component)
	}
	cr := &Created{Transfer: page.Props.Transfer, byName: map[string]string{}}
	for _, f := range cr.Transfer.Files {
		cr.byName[f.Path] = f.ID
	}
	return cr, nil
}

// FileID returns the server id for an announced path.
func (cr *Created) FileID(name string) (string, bool) {
	id, ok := cr.byName[name]
	return id, ok
}

// Progress receives transferred byte counts.
type Progress interface {
	Add(n int64)
	SetLabel(s string)
}

type noProgress struct{}

func (noProgress) Add(int64)       {}
func (noProgress) SetLabel(string) {}

// UploadFile uploads one local file in 50 MiB chunks, `parallel` chunks at a time, then finalises it.
func (c *Client) UploadFile(ctx context.Context, transferID, fileID string, lf LocalFile, parallel int, prog Progress) error {
	if prog == nil {
		prog = noProgress{}
	}
	if parallel < 1 {
		parallel = 1
	}
	f, err := os.Open(lf.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	n := int((lf.Size + ChunkSize - 1) / ChunkSize)
	if n < 1 {
		n = 1
	}
	prog.SetLabel(lf.Name)

	etags := make(map[int]string, n)
	var mu sync.Mutex
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for start := 1; start <= n; start += urlBatch {
		end := start + urlBatch - 1
		if end > n {
			end = n
		}
		var chunks []*int
		if n == 1 {
			chunks = []*int{nil}
		} else {
			for i := start; i <= end; i++ {
				i := i
				chunks = append(chunks, &i)
			}
		}
		urls, err := c.uploadURLs(ctx, transferID, fileID, chunks)
		if err != nil {
			return err
		}
		sem := make(chan struct{}, parallel)
		var wg sync.WaitGroup
		var firstErr error
		for idx, ch := range chunks {
			i := start + idx // 1-based chunk index
			key := chunkKey(ch)
			u, ok := urls[key]
			if !ok {
				return newErr(KindGeneric, 0, "no upload URL returned for chunk %d of %s", i, lf.Name)
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, ch *int, u string) {
				defer wg.Done()
				defer func() { <-sem }()
				etag, err := c.putChunk(ctx, transferID, fileID, ch, u, f, lf.Size, i, n, prog)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					return
				}
				etags[i] = etag
			}(i, ch, u)
		}
		wg.Wait()
		if firstErr != nil {
			return firstErr
		}
	}

	var body any = map[string]any{}
	if n > 1 {
		type et struct {
			ChunkIndex int    `json:"chunk_index"`
			Etag       string `json:"etag"`
		}
		list := make([]et, 0, n)
		idx := make([]int, 0, n)
		for i := range etags {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		for _, i := range idx {
			list = append(list, et{i, etags[i]})
		}
		body = map[string]any{"etags": list}
	}
	if _, err := c.doJSON(ctx, "PATCH", fmt.Sprintf("/api/1/transfers/%s/files/%s", transferID, fileID), nil, body, false, nil, nil); err != nil {
		return fmt.Errorf("finalise %s: %w", lf.Name, err)
	}
	return nil
}

func chunkKey(ch *int) int {
	if ch == nil {
		return 0
	}
	return *ch
}

func (c *Client) uploadURLs(ctx context.Context, transferID, fileID string, chunks []*int) (map[int]string, error) {
	type reqItem struct {
		File  string `json:"file"`
		Chunk *int   `json:"chunk"`
	}
	items := make([]reqItem, 0, len(chunks))
	for _, ch := range chunks {
		items = append(items, reqItem{File: fileID, Chunk: ch})
	}
	var out apiEnvelope[[]uploadURL]
	_, err := c.doJSON(ctx, "POST", fmt.Sprintf("/api/1/transfers/%s/files", transferID), nil,
		map[string]any{"upload_url_requests": items}, false, nil, &out)
	if err != nil {
		return nil, fmt.Errorf("request upload URLs: %w", err)
	}
	m := make(map[int]string, len(out.Data))
	for _, u := range out.Data {
		m[chunkKey(u.Chunk)] = u.URL
	}
	return m, nil
}

// putChunk uploads chunk i (1-based) of the file to a presigned URL and returns the raw ETag header.
// On failure it retries up to 3 times, requesting a fresh URL (they expire after one hour).
func (c *Client) putChunk(ctx context.Context, transferID, fileID string, ch *int, u string, f *os.File, size int64, i, n int, prog Progress) (string, error) {
	offset := int64(i-1) * ChunkSize
	length := size - offset
	if length > ChunkSize {
		length = ChunkSize
	}
	if length < 0 {
		length = 0
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if !sleepCtx(ctx, backoff(attempt-1)) {
				return "", ctx.Err()
			}
			urls, err := c.uploadURLs(ctx, transferID, fileID, []*int{ch})
			if err != nil {
				lastErr = err
				continue
			}
			u = urls[chunkKey(ch)]
		}
		sent := int64(0)
		etag, err := c.putOnce(ctx, u, f, offset, length, func(k int64) { sent += k; prog.Add(k) })
		if err == nil {
			return etag, nil
		}
		prog.Add(-sent) // roll back progress for the retry
		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		c.log("chunk %d/%d of %s failed (attempt %d/3): %v", i, n, fileID, attempt+1, err)
	}
	return "", fmt.Errorf("upload chunk %d/%d: %w", i, n, lastErr)
}

type countingReader struct {
	r  io.Reader
	fn func(int64)
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.fn(int64(n))
	}
	return n, err
}

func (c *Client) putOnce(ctx context.Context, u string, f *os.File, offset, length int64, onRead func(int64)) (string, error) {
	body := &countingReader{r: io.NewSectionReader(f, offset, length), fn: onRead}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, body)
	if err != nil {
		return "", err
	}
	req.ContentLength = length
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.raw.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("S3 PUT: HTTP %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", errors.New("S3 PUT: missing ETag header")
	}
	return etag, nil
}

// Complete closes the transfer and returns the download link.
func (c *Client) Complete(ctx context.Context, transferID string) (*Link, error) {
	var out apiEnvelope[struct {
		Link Link `json:"link"`
	}]
	if _, err := c.doJSON(ctx, "PATCH", "/api/1/transfers/"+transferID, nil, map[string]string{"status": "completed"}, false, nil, &out); err != nil {
		return nil, fmt.Errorf("complete transfer: %w", err)
	}
	if out.Data.Link.DownloadURL == "" {
		return nil, newErr(KindGeneric, 200, "transfer completed but no download URL was returned")
	}
	return &out.Data.Link, nil
}

// Cancel marks an unfinished transfer as cancelled. It uses its own short deadline so it still
// runs when the parent context was interrupted.
func (c *Client) Cancel(transferID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.doJSON(ctx, "PATCH", "/api/1/transfers/"+transferID, nil, map[string]string{"status": "cancelled"}, false, nil, nil)
	return err
}
