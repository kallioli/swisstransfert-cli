package swisstransfer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var uuidRe = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// ParseLink extracts the link id from a SwissTransfer URL or a bare uuid.
func ParseLink(s string) (string, error) {
	s = strings.TrimSpace(s)
	if uuidRe.MatchString(s) && len(s) == 36 {
		return strings.ToLower(s), nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid link %q", s)
	}
	if !strings.HasSuffix(u.Host, "swisstransfer.com") && !strings.HasSuffix(u.Host, "swisstransfer.infomaniak.com") {
		return "", fmt.Errorf("not a SwissTransfer link: %s", s)
	}
	if m := uuidRe.FindString(u.Path); m != "" && strings.Contains(u.Path, "/dl/") {
		return strings.ToLower(m), nil
	}
	return "", fmt.Errorf("no transfer id found in %s", s)
}

// Open fetches the download page of a link. Returns the transfer when it is not password
// protected; otherwise returns (nil, true, nil).
func (c *Client) Open(ctx context.Context, linkID string) (t *Transfer, protected bool, err error) {
	page, err := c.GetPage(ctx, "/dl/"+linkID)
	if err != nil {
		return nil, false, err
	}
	return c.pageTransfer(page)
}

func (c *Client) pageTransfer(page *Page) (*Transfer, bool, error) {
	switch {
	case strings.HasSuffix(page.Component, "/password"):
		return nil, true, nil
	case page.Props.Transfer != nil && page.Props.Transfer.ID != "":
		return page.Props.Transfer, false, nil
	case strings.Contains(page.Component, "expired") || strings.Contains(page.Component, "deleted"):
		return nil, false, newErr(KindNotFound, 410, "this transfer has expired or was deleted")
	case strings.Contains(page.Component, "virus") || strings.Contains(page.Component, "blocked"):
		return nil, false, newErr(KindGeneric, 403, "this transfer is unavailable (%s)", page.Component)
	}
	return nil, false, newErr(KindGeneric, 200, "unexpected page %q", page.Component)
}

// Unlock submits the password of a protected link (Open must have been called first).
func (c *Client) Unlock(ctx context.Context, linkID, password string) (*Transfer, error) {
	var page Page
	_, err := c.doJSON(ctx, "POST", "/dl/"+linkID, nil, map[string]string{"password": password}, true, nil, &page)
	if err != nil {
		var e *Error
		if asErr(err, &e) {
			switch {
			case e.Kind == KindValidation:
				return nil, &Error{Kind: KindPassword, Status: e.Status, Message: "wrong password"}
			case e.Status >= 300 && e.Status < 400:
				// Laravel redirects back to the page with the validation error flashed in the session.
				p, gerr := c.GetPage(ctx, "/dl/"+linkID)
				if gerr != nil {
					return nil, gerr
				}
				if _, ok := p.Props.Errors["password"]; ok || strings.HasSuffix(p.Component, "/password") {
					return nil, &Error{Kind: KindPassword, Status: 422, Message: "wrong password"}
				}
				t, protected, perr := c.pageTransfer(p)
				if perr == nil && !protected {
					return t, nil
				}
				return nil, &Error{Kind: KindPassword, Status: 422, Message: "wrong password"}
			}
		}
		return nil, err
	}
	c.absorb(&page)
	if len(page.Props.Errors) > 0 {
		return nil, &Error{Kind: KindPassword, Status: 422, Message: "wrong password"}
	}
	t, protected, err := c.pageTransfer(&page)
	if err != nil {
		return nil, err
	}
	if protected {
		return nil, &Error{Kind: KindPassword, Status: 200, Message: "wrong password"}
	}
	return t, nil
}

// FileURL returns the presigned S3 URL of a file (valid for about one hour).
func (c *Client) FileURL(ctx context.Context, linkID, fileID string) (string, error) {
	var out apiEnvelope[struct {
		URL string `json:"url"`
	}]
	_, err := c.doJSON(ctx, "GET", fmt.Sprintf("/api/1/links/%s/files/%s", linkID, fileID), nil, nil, false, nil, &out)
	if err != nil {
		var e *Error
		if asErr(err, &e) && e.Status == 403 {
			return "", &Error{Kind: KindPassword, Status: 403, Message: "this transfer is password protected"}
		}
		return "", err
	}
	if out.Data.URL == "" {
		return "", newErr(KindGeneric, 200, "no download URL returned")
	}
	return out.Data.URL, nil
}

// Fetch streams a presigned URL into w and returns the number of bytes written.
func (c *Client) Fetch(ctx context.Context, u string, w io.Writer, prog Progress) (int64, error) {
	if prog == nil {
		prog = noProgress{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.raw.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	return io.Copy(w, &countingReader{r: resp.Body, fn: prog.Add})
}

func asErr(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
