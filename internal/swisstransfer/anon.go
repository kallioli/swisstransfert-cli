package swisstransfer

import (
	"context"
	"time"
)

// EmailValidation is a pending sender-address verification for anonymous transfers.
type EmailValidation struct {
	ID             string `json:"id"`
	TokenExpiresAt int64  `json:"token_expires_at"`
}

// Expiry returns when the emailed code stops being valid.
func (v *EmailValidation) Expiry() time.Time { return time.Unix(v.TokenExpiresAt, 0) }

// RequestEmailValidation asks the server to e-mail a 6-character code to the sender address.
func (c *Client) RequestEmailValidation(ctx context.Context, email string) (*EmailValidation, error) {
	if c.version == "" {
		if _, err := c.Bootstrap(ctx); err != nil {
			return nil, err
		}
	}
	var out apiEnvelope[EmailValidation]
	_, err := c.doJSON(ctx, "POST", "/api/1/email-validations", nil, map[string]string{"email": email}, false, nil, &out)
	if err != nil {
		var e *Error
		if asErr(err, &e) && e.Code == "email_validation_rate_limit_error" {
			return nil, &Error{Kind: KindGeneric, Status: e.Status, Code: e.Code, Message: "too many validation requests for this address, try again later"}
		}
		return nil, err
	}
	if out.Data.ID == "" {
		return nil, newErr(KindGeneric, 201, "no validation id returned")
	}
	return &out.Data, nil
}

// ConfirmEmailValidation submits the code received by e-mail. A wrong code yields an *Error
// with Kind KindPassword and Code "invalid_validation_token".
func (c *Client) ConfirmEmailValidation(ctx context.Context, id, code string) error {
	_, err := c.doJSON(ctx, "POST", "/api/1/email-validations/"+id+"/confirm", nil, map[string]string{"token": code}, false, nil, nil)
	if err != nil {
		var e *Error
		if asErr(err, &e) {
			switch e.Code {
			case "invalid_validation_token":
				return &Error{Kind: KindPassword, Status: e.Status, Code: e.Code, Message: "invalid code"}
			case "email_validation_request_locked", "email_validation_rate_limit_error":
				return &Error{Kind: KindGeneric, Status: e.Status, Code: e.Code, Message: "too many attempts, request a new code later"}
			}
		}
		return err
	}
	return nil
}

// ResendEmailValidation asks for the code to be sent again.
func (c *Client) ResendEmailValidation(ctx context.Context, id string) error {
	_, err := c.doJSON(ctx, "POST", "/api/1/email-validations/"+id+"/resend", nil, nil, false, nil, nil)
	return err
}

// EmailValidated reports whether a previously confirmed validation id is still accepted.
func (c *Client) EmailValidated(ctx context.Context, id, email string) (bool, error) {
	if c.version == "" {
		if _, err := c.Bootstrap(ctx); err != nil {
			return false, err
		}
	}
	var out apiEnvelope[struct {
		IsValidated bool `json:"is_validated"`
	}]
	q := map[string][]string{"email": {email}}
	if _, err := c.doJSON(ctx, "GET", "/api/1/email-validations/"+id, q, nil, false, nil, &out); err != nil {
		return false, err
	}
	return out.Data.IsValidated, nil
}
