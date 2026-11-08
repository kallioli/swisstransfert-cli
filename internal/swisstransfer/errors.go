package swisstransfer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Kind classifies errors so the CLI can map them to exit codes.
type Kind int

const (
	KindGeneric    Kind = iota
	KindAuth            // token missing/invalid, space id required
	KindNotFound        // link unknown or expired
	KindPassword        // password required or wrong
	KindValidation      // 422 with field errors
)

// Error is a protocol-level error.
type Error struct {
	Kind    Kind
	Status  int
	Code    string // machine-readable API code when available (e.g. invalid_validation_token)
	Message string
	Fields  map[string][]string // validation errors by field
	Hint    string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if len(e.Fields) > 0 {
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("\n  %s: %s", k, strings.Join(e.Fields[k], "; ")))
		}
	}
	if e.Hint != "" {
		b.WriteString("\n  hint: " + e.Hint)
	}
	return b.String()
}

func newErr(kind Kind, status int, format string, a ...any) *Error {
	return &Error{Kind: kind, Status: status, Message: fmt.Sprintf(format, a...)}
}

// parseAPIError turns a non-2xx response body into an *Error.
func parseAPIError(status int, body []byte, context string) *Error {
	e := &Error{Kind: KindGeneric, Status: status, Message: fmt.Sprintf("%s: HTTP %d", context, status)}
	if status == 422 {
		e.Kind = KindValidation
	}
	// Laravel web validation: {"message": "...", "errors": {"field": ["msg"]}}
	var web struct {
		Message string              `json:"message"`
		Errors  map[string][]string `json:"errors"`
	}
	if json.Unmarshal(body, &web) == nil && (web.Message != "" || len(web.Errors) > 0) {
		if web.Message != "" {
			e.Message = fmt.Sprintf("%s: %s", context, web.Message)
		}
		e.Fields = web.Errors
		return e
	}
	// API error: {"result":"error","error":{"code","description","errors":[{code,description,context}]}}
	var api struct {
		Result string `json:"result"`
		Error  struct {
			Code        string `json:"code"`
			Description string `json:"description"`
			Errors      []struct {
				Code        string         `json:"code"`
				Description string         `json:"description"`
				Context     map[string]any `json:"context"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &api) == nil && api.Error.Code != "" {
		e.Code = api.Error.Code
		e.Message = fmt.Sprintf("%s: %s", context, api.Error.Code)
		if api.Error.Description != "" {
			e.Message += " (" + api.Error.Description + ")"
		}
		if len(api.Error.Errors) > 0 {
			e.Fields = map[string][]string{}
			for _, sub := range api.Error.Errors {
				key := sub.Code
				if attr, ok := sub.Context["attribute"].(string); ok {
					key = attr
				}
				e.Fields[key] = append(e.Fields[key], sub.Description)
			}
		}
		if api.Error.Code == "access_denied" {
			e.Kind = KindAuth
		}
		return e
	}
	if s := strings.TrimSpace(string(body)); s != "" && len(s) < 200 && !strings.HasPrefix(s, "<") {
		e.Message += ": " + s
	}
	return e
}
