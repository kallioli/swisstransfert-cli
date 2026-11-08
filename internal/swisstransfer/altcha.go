package swisstransfer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// solveAltcha solves the proof-of-work: find n in [0, maxNumber] with sha256(salt+n) == challenge.
// The search is split across CPU cores.
func solveAltcha(ctx context.Context, ch altchaChallenge) (string, error) {
	if ch.Algorithm != "" && ch.Algorithm != "SHA-256" {
		return "", fmt.Errorf("unsupported altcha algorithm %q", ch.Algorithm)
	}
	target, err := hex.DecodeString(ch.Challenge)
	if err != nil {
		return "", fmt.Errorf("altcha: bad challenge: %w", err)
	}
	start := time.Now()
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	var found atomic.Int64
	found.Store(-1)
	var wg sync.WaitGroup
	salt := []byte(ch.Salt)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			buf := make([]byte, 0, len(salt)+20)
			for n := offset; n <= ch.MaxNumber; n += workers {
				if n&0xfff == 0 {
					if found.Load() >= 0 || ctx.Err() != nil {
						return
					}
				}
				buf = append(buf[:0], salt...)
				buf = strconv.AppendInt(buf, int64(n), 10)
				sum := sha256.Sum256(buf)
				if string(sum[:]) == string(target) {
					found.CompareAndSwap(-1, int64(n))
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	n := found.Load()
	if n < 0 {
		return "", fmt.Errorf("altcha: no solution found up to %d", ch.MaxNumber)
	}
	payload, _ := json.Marshal(map[string]any{
		"algorithm": "SHA-256",
		"challenge": ch.Challenge,
		"number":    n,
		"salt":      ch.Salt,
		"signature": ch.Signature,
		"took":      time.Since(start).Milliseconds(),
	})
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (c *Client) altcha(ctx context.Context) (string, error) {
	var ch altchaChallenge
	if _, err := c.doJSON(ctx, "GET", "/api/1/altcha-challenge", nil, nil, false, nil, &ch); err != nil {
		return "", err
	}
	c.log("altcha challenge maxNumber=%d", ch.MaxNumber)
	return solveAltcha(ctx, ch)
}
