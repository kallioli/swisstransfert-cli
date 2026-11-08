// Package swisstransfer implements the (unofficial) SwissTransfer web protocol:
// session bootstrap, Altcha proof of work, transfer creation, chunked S3 upload and download.
//
// Everything here was reverse-engineered from the web app and may break without notice.
package swisstransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// SendBase is the authenticated app used to create transfers.
	SendBase = "https://swisstransfer.infomaniak.com"
	// DownloadBase serves public download pages.
	DownloadBase = "https://www.swisstransfer.com"

	xsrfCookie = "SWISSTRANSFER-API-XSRF-TOKEN"
	// ChunkSize is the S3 multipart chunk size imposed by the server.
	ChunkSize = 50 * 1024 * 1024
	// urlBatch is the maximum number of upload URL requests per call (422 above).
	urlBatch = 10
)

// Logger receives debug lines when verbose mode is on.
type Logger func(format string, a ...any)

// Client talks to one SwissTransfer host with a persistent cookie session.
type Client struct {
	base      *url.URL
	http      *http.Client // app requests: cookie jar, no redirects
	raw       *http.Client // S3 requests: no cookies, no auth
	token     string
	spaceID   string
	userAgent string
	version   string // Inertia version of the last page seen
	limits    *Limits
	user      *User
	log       Logger
}

// Options configures a Client.
type Options struct {
	Base      string // SendBase or DownloadBase
	Token     string // Infomaniak API token (sending only)
	SpaceID   string // kSuite space id (optional)
	UserAgent string
	Log       Logger
}

// New builds a client. It performs no network call.
func New(o Options) (*Client, error) {
	base, err := url.Parse(o.Base)
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}
	c := &Client{
		base:      base,
		token:     o.Token,
		spaceID:   o.SpaceID,
		userAgent: o.UserAgent,
		log:       o.Log,
		http: &http.Client{
			Jar:       jar,
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		raw: &http.Client{Transport: transport},
	}
	if c.userAgent == "" {
		c.userAgent = "swisstransfer-cli"
	}
	if c.log == nil {
		c.log = func(string, ...any) {}
	}
	return c, nil
}

// Limits returns the account limits read from the last page (nil before Bootstrap).
func (c *Client) Limits() *Limits { return c.limits }

// User returns the authenticated user read from the last page (nil if anonymous).
func (c *Client) User() *User { return c.user }

func (c *Client) pageQuery() url.Values {
	v := url.Values{}
	if c.spaceID != "" {
		v.Set("ksuite-mode", "main")
		v.Set("space-id", c.spaceID)
	}
	return v
}

func (c *Client) xsrf() string {
	for _, ck := range c.http.Jar.Cookies(c.base) {
		if ck.Name == xsrfCookie {
			if v, err := url.QueryUnescape(ck.Value); err == nil {
				return v
			}
			return ck.Value
		}
	}
	return ""
}

func (c *Client) setHeaders(req *http.Request, inertia bool) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if x := c.xsrf(); x != "" {
		req.Header.Set("X-XSRF-TOKEN", x)
	}
	if inertia {
		req.Header.Set("X-Inertia", "true")
		req.Header.Set("X-Inertia-Version", c.version)
	}
}

var pageRe = regexp.MustCompile(`(?s)data-page="app"\s+type="application/json">(.*?)</script>`)

// GetPage fetches an HTML page, stores the session cookies and returns the embedded Inertia page.
func (c *Client) GetPage(ctx context.Context, path string) (*Page, error) {
	u := c.base.ResolveReference(&url.URL{Path: path})
	if q := c.pageQuery(); len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	var resp *http.Response
	var body []byte
	for hop := 0; ; hop++ {
		resp, body, err = c.doRetry(ctx, c.http, req, nil)
		if err != nil {
			return nil, err
		}
		c.log("GET %s -> %d (%d bytes)", req.URL.Path, resp.StatusCode, len(body))
		if resp.StatusCode < 300 || resp.StatusCode > 399 || hop >= 2 {
			break
		}
		// Follow same-host redirects (e.g. "/" -> "/en"); anything else is an auth problem.
		loc, perr := url.Parse(resp.Header.Get("Location"))
		if perr != nil || (loc.Host != "" && loc.Host != c.base.Host) {
			break
		}
		next := req.URL.ResolveReference(loc)
		if q := c.pageQuery(); len(q) > 0 && next.RawQuery == "" {
			next.RawQuery = q.Encode()
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, next.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "text/html,application/json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}
	switch {
	case resp.StatusCode == 302 || resp.StatusCode == 301 || resp.StatusCode == 303:
		loc := resp.Header.Get("Location")
		return nil, c.redirectError(loc)
	case resp.StatusCode == 404:
		return nil, newErr(KindNotFound, 404, "transfer not found or expired")
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, &Error{Kind: KindAuth, Status: resp.StatusCode, Message: fmt.Sprintf("access denied (HTTP %d)", resp.StatusCode), Hint: "check your API token"}
	case resp.StatusCode >= 400:
		return nil, parseAPIError(resp.StatusCode, body, "GET "+path)
	}
	page, err := parsePage(body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	c.absorb(page)
	return page, nil
}

func (c *Client) redirectError(loc string) *Error {
	switch {
	case strings.Contains(loc, "ksuite.infomaniak.com"):
		return &Error{Kind: KindAuth, Status: 302, Message: "this account is a kSuite account: a space id is required",
			Hint: "run `swisstransfer login --space-id <id>` or set SWISSTRANSFER_SPACE_ID (the id appears in the kSuite URL: ksuite.infomaniak.com/<id>/...)"}
	case strings.Contains(loc, "login.infomaniak.com"):
		if c.token == "" {
			return &Error{Kind: KindAuth, Status: 302, Message: "an Infomaniak API token is required to send files",
				Hint: "run `swisstransfer login` (token: manager.infomaniak.com → Developer → API tokens)"}
		}
		return &Error{Kind: KindAuth, Status: 302, Message: "the API token was rejected (redirected to login)",
			Hint: "check the token in `swisstransfer config show` or create a new one in the Infomaniak manager"}
	}
	return &Error{Kind: KindGeneric, Status: 302, Message: "unexpected redirect to " + loc}
}

func parsePage(body []byte, contentType string) (*Page, error) {
	var raw []byte
	if strings.Contains(contentType, "application/json") {
		raw = body
	} else {
		m := pageRe.FindSubmatch(body)
		if m == nil {
			return nil, newErr(KindGeneric, 200, "unexpected response: no Inertia page found (the SwissTransfer web app may have changed)")
		}
		raw = m[1]
	}
	var p Page
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse page: %w", err)
	}
	return &p, nil
}

func (c *Client) absorb(p *Page) {
	if p.Version != "" {
		c.version = p.Version
	}
	if p.Props.Limits != nil {
		c.limits = p.Props.Limits
	}
	if p.Props.User != nil {
		c.user = p.Props.User
	}
}

// Bootstrap initialises the session (cookies, XSRF token, Inertia version, limits, user).
func (c *Client) Bootstrap(ctx context.Context) (*Page, error) {
	return c.GetPage(ctx, "/")
}

// doJSON sends a JSON request against the app and decodes the JSON response into out.
// It retries transparently on 419 (CSRF) and 409 (stale Inertia version) after a re-bootstrap.
func (c *Client) doJSON(ctx context.Context, method, path string, q url.Values, body any, inertia bool, extra http.Header, out any) (int, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return 0, err
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		u := c.base.ResolveReference(&url.URL{Path: path})
		if len(q) > 0 {
			u.RawQuery = q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return 0, err
		}
		c.setHeaders(req, inertia)
		for k, v := range extra {
			req.Header[k] = v
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, respBody, err := c.doRetry(ctx, c.http, req, payload)
		if err != nil {
			return 0, err
		}
		c.log("%s %s -> %d (%d bytes)", method, u.Path, resp.StatusCode, len(respBody))
		if (resp.StatusCode == 419 || resp.StatusCode == 409) && attempt == 0 {
			c.log("session refresh after HTTP %d", resp.StatusCode)
			if _, err := c.Bootstrap(ctx); err != nil {
				return resp.StatusCode, err
			}
			continue
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return resp.StatusCode, c.redirectError(resp.Header.Get("Location"))
		}
		if resp.StatusCode >= 400 {
			return resp.StatusCode, parseAPIError(resp.StatusCode, respBody, method+" "+path)
		}
		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return resp.StatusCode, fmt.Errorf("%s %s: invalid JSON response: %w", method, path, err)
			}
		}
		return resp.StatusCode, nil
	}
	return 0, errors.New("unreachable")
}

// doRetry performs the request with retries on network errors, 429 and 5xx (idempotent-safe for
// our calls: every write is either repeatable or guarded upstream). The body is re-sent each time.
func (c *Client) doRetry(ctx context.Context, hc *http.Client, req *http.Request, payload []byte) (*http.Response, []byte, error) {
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		if payload != nil {
			req.Body = io.NopCloser(bytes.NewReader(payload))
			req.ContentLength = int64(len(payload))
			req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }
		}
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			c.log("request error (attempt %d/%d): %v", i+1, attempts, err)
			if !sleepCtx(ctx, backoff(i)) {
				return nil, nil, ctx.Err()
			}
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			wait := backoff(i)
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if s, err := strconv.Atoi(ra); err == nil && s > 0 && s <= 60 {
					wait = time.Duration(s) * time.Second
				}
			}
			if i < attempts-1 {
				c.log("HTTP %d, retrying in %s", resp.StatusCode, wait)
				if !sleepCtx(ctx, wait) {
					return nil, nil, ctx.Err()
				}
				continue
			}
		}
		return resp, body, nil
	}
	return nil, nil, fmt.Errorf("request failed after %d attempts: %w", attempts, lastErr)
}

func backoff(i int) time.Duration { return time.Duration(1<<uint(i)) * time.Second }

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
