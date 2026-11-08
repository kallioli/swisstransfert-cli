package swisstransfer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"
)

func TestSolveAltcha(t *testing.T) {
	salt := "abc?expires=1794168000&"
	n := 12345
	sum := sha256.Sum256([]byte(salt + strconv.Itoa(n)))
	ch := altchaChallenge{Algorithm: "SHA-256", Challenge: hex.EncodeToString(sum[:]), Salt: salt, MaxNumber: 100000, Signature: "sig"}
	out, err := solveAltcha(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Number    int    `json:"number"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Number != n || payload.Signature != "sig" {
		t.Fatalf("got %+v", payload)
	}
}

func TestParseLink(t *testing.T) {
	cases := map[string]string{
		"https://www.swisstransfer.com/dl/01a1bb9a-763c-728b-9984-b114dfb634fd":    "01a1bb9a-763c-728b-9984-b114dfb634fd",
		"https://www.swisstransfer.com/fr/dl/01A1BB9A-763C-728B-9984-B114DFB634FD": "01a1bb9a-763c-728b-9984-b114dfb634fd",
		"swisstransfer.infomaniak.com/dl/01a1bb9a-763c-728b-9984-b114dfb634fd?x=1": "01a1bb9a-763c-728b-9984-b114dfb634fd",
		"01a1bb9a-763c-728b-9984-b114dfb634fd":                                     "01a1bb9a-763c-728b-9984-b114dfb634fd",
	}
	for in, want := range cases {
		got, err := ParseLink(in)
		if err != nil || got != want {
			t.Errorf("ParseLink(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://example.com/dl/01a1bb9a-763c-728b-9984-b114dfb634fd", "not a link", "https://www.swisstransfer.com/"} {
		if _, err := ParseLink(bad); err == nil {
			t.Errorf("ParseLink(%q) should fail", bad)
		}
	}
}

func TestParseAPIError(t *testing.T) {
	e := parseAPIError(422, []byte(`{"result":"error","error":{"code":"validation_failed","description":"Validation failed","errors":[{"code":"validation_rule_max","description":"too many","context":{"attribute":"upload_url_requests"}}]}}`), "POST x")
	if e.Kind != KindValidation || e.Code != "validation_failed" || e.Fields["upload_url_requests"][0] != "too many" {
		t.Fatalf("unexpected %+v", e)
	}
	w := parseAPIError(422, []byte(`{"message":"The recipients field must be an array.","errors":{"recipients":["The recipients field must be an array."]}}`), "POST /en")
	if len(w.Fields["recipients"]) != 1 {
		t.Fatalf("unexpected %+v", w)
	}
}
