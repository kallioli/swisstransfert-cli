package swisstransfer

import "time"

// Page is an Inertia.js page payload.
type Page struct {
	Component string    `json:"component"`
	Props     PageProps `json:"props"`
	URL       string    `json:"url"`
	Version   string    `json:"version"`
}

// PageProps holds the subset of Inertia props the CLI uses.
type PageProps struct {
	Errors   map[string]any `json:"errors"`
	User     *User          `json:"user"`
	Limits   *Limits        `json:"limits"`
	Transfer *Transfer      `json:"transfer"`
	Link     *struct {
		ID        string `json:"id"`
		ReportURL string `json:"report_url"`
	} `json:"link"`
}

// User is the authenticated Infomaniak user.
type User struct {
	ID        int    `json:"id"`
	Email     string `json:"email"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
}

// Limits are the account's allowed values.
type Limits struct {
	ExpiryDays    []int `json:"expiry_days"`
	DownloadLimit []int `json:"download_limit"`
}

// Transfer describes a transfer (created or being downloaded).
type Transfer struct {
	ID        string  `json:"id"`
	Title     *string `json:"title"`
	Message   *string `json:"message"`
	TotalSize int64   `json:"total_size"`
	ExpiresAt int64   `json:"expires_at"`
	Type      string  `json:"type"`
	Files     []File  `json:"files"`
}

// Expiry returns the expiration time.
func (t *Transfer) Expiry() time.Time { return time.Unix(t.ExpiresAt, 0) }

// File is a file inside a transfer.
type File struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	MimeType      string `json:"mime_type"`
	MimeTypeGroup string `json:"mime_type_group,omitempty"`
}

// FileDeclaration is a file announced when creating a transfer.
type FileDeclaration struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

// CreateRequest is the body of POST /.
type CreateRequest struct {
	Method        string            `json:"method"`
	Title         string            `json:"title"`
	Recipients    []string          `json:"recipients"`
	Email         string            `json:"email"`
	Message       string            `json:"message"`
	Password      string            `json:"password"`
	Language      string            `json:"language"`
	MaxDownload   int               `json:"max_download"`
	ExpiresInDays int               `json:"expires_in_days"`
	Altcha        string            `json:"altcha"`
	Files         []FileDeclaration `json:"files"`
	// EmailValidationID is set for anonymous transfers once the sender address was confirmed.
	EmailValidationID string `json:"email_validation_id,omitempty"`
}

// Link is the result of a completed transfer.
type Link struct {
	ID          string `json:"id"`
	DownloadURL string `json:"download_url"`
	ReportURL   string `json:"report_url"`
}

type altchaChallenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
	MaxNumber int    `json:"maxNumber"`
	Signature string `json:"signature"`
}

type uploadURL struct {
	File  string `json:"file"`
	Chunk *int   `json:"chunk"`
	URL   string `json:"url"`
}

type apiEnvelope[T any] struct {
	Result string `json:"result"`
	Data   T      `json:"data"`
}
