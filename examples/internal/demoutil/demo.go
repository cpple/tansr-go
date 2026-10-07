// Package demoutil contains command-line presentation and credential loading,
// never SDK protocol or execution logic.
package demoutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tansrai/tansr-go/api"
)

// Client reads a short-lived user token for each request. A developer's trusted
// login service can replace TANSR_TOKEN_FILE when renewing that same principal.
func Client(base string) (*api.Client, error) {
	if os.Getenv("TANSR_TOKEN") == "" && os.Getenv("TANSR_TOKEN_FILE") == "" {
		return nil, errors.New("set TANSR_TOKEN or TANSR_TOKEN_FILE to a short-lived Serve user token")
	}
	return api.New(api.Options{BaseURL: base, SessionFamily: "sdk1", EventEnvelope: true, TokenFunc: token})
}

func token(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if path := os.Getenv("TANSR_TOKEN_FILE"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return "", errors.New("cannot open TANSR_TOKEN_FILE")
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, 8193))
		if err != nil || len(body) > 8192 {
			return "", errors.New("cannot read a bounded token from TANSR_TOKEN_FILE")
		}
		return strings.TrimRight(string(body), "\r\n"), nil
	}
	return os.Getenv("TANSR_TOKEN"), nil
}

func RequestID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "go-" + hex.EncodeToString(random[:]), nil
}

// Describe avoids echoing response bodies, business arguments, or credentials.
func Describe(err error) error {
	var remote *api.APIError
	if errors.As(err, &remote) {
		return fmt.Errorf("Serve code=%s retryAction=%s domainCode=%s", remote.Code, remote.RetryAction, remote.Detail.DomainCode)
	}
	return err
}

// Text excludes terminal escape controls from untrusted display text.
func Text(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 && !(r >= 128 && r <= 159) {
			return r
		}
		return -1
	}, value)
}
