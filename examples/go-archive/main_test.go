package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tansrai/tansr-go/archive"
)

func TestArchiveKeyAndInvalidOptionsFailBeforeNetwork(t *testing.T) {
	for _, value := range []string{"", "secret", strings.Repeat("x", 64), strings.Repeat("00", 31)} {
		t.Setenv("TANSR_ARCHIVE_KEY", value)
		if _, err := archiveKey(); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	t.Setenv("TANSR_ARCHIVE_KEY", strings.Repeat("ab", 32))
	key, err := archiveKey()
	if err != nil || len(key) != 32 {
		t.Fatal(err)
	}
	for _, opts := range []options{{}, {path: "data", session: "s", binding: "b", maxPages: 1}, {path: "data", session: "s", maxPages: 0}} {
		if err := run(context.Background(), opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}

func TestRecoveryFlagIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		bad  bool
	}{
		{name: "normal-sync"},
		{name: "explicit-recovery", args: []string{"-recover-ack", "recovery-original-1"}, want: "recovery-original-1"},
		{name: "missing-id", args: []string{"-recover-ack"}, bad: true},
		{name: "empty-id", args: []string{"-recover-ack", ""}, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts options
			flags := flag.NewFlagSet("go-archive", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			bindFlags(flags, &opts)
			err := flags.Parse(tc.args)
			if (err != nil) != tc.bad || opts.recoverAck != tc.want || opts.maxPages != 64 {
				t.Fatalf("parse err=%v recover=%q maxPages=%d", err, opts.recoverAck, opts.maxPages)
			}
		})
	}
}

func TestRecoveryRequiresExistingBindingAndFileBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("TANSR_ARCHIVE_KEY", strings.Repeat("ab", 32))
	t.Setenv("TANSR_TOKEN", "synthetic-test-token")
	t.Setenv("TANSR_TOKEN_FILE", "")
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing", "archive.bin")
	for _, tc := range []struct {
		name, session, binding, path, want string
	}{
		{"no-binding", "session-1", "", missing, "requires -binding"},
		{"no-file", "", "binding-1", missing, "existing regular archive file"},
		{"directory-is-not-file", "", "binding-1", directory, "existing regular archive file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(context.Background(), options{base: server.URL, session: tc.session, binding: tc.binding, path: tc.path,
				maxPages: 64, recoverAck: "recovery-1"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected preflight %q, got %v", tc.want, err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("invalid recovery attempted a network request")
	}
	if _, err := os.Stat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery created a new archive directory: %v", err)
	}
}

// These are presentation assertions, not protocol-success fixtures. Actual
// recovery, receipt validation and format migration are exercised by archive
// tests and the real Serve integration tests.
func TestRecoveryDisplayRequiresConfirmedResult(t *testing.T) {
	completed := archive.SyncResult{Recovered: true, Receipt: &archive.MutationReceipt{
		State: "completed", Request: archive.RequestIdentity{RequestID: "saved-original-identity"},
	}}
	var output bytes.Buffer
	if err := showRecoveryResult(&output, completed, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "saved-original-identity") || !strings.Contains(output.String(), "rerun without -recover-ack") || strings.Contains(output.String(), "archive synchronized") {
		t.Fatalf("recovery was presented as a full sync: %s", output.String())
	}
	failed := errors.New("synthetic recovery transport failure")
	for _, tc := range []struct {
		name   string
		result archive.SyncResult
		err    error
		want   error
	}{
		{name: "failed", result: completed, err: failed, want: failed},
		{name: "no-receipt", result: archive.SyncResult{Recovered: true}, want: archive.ErrReceipt},
		{name: "not-recovered", result: archive.SyncResult{Receipt: completed.Receipt}, want: archive.ErrReceipt},
		{name: "only-accepted", result: archive.SyncResult{Recovered: true, Receipt: &archive.MutationReceipt{State: "accepted"}}, want: archive.ErrReceipt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := showRecoveryResult(&output, tc.result, tc.err)
			if !errors.Is(err, tc.want) || output.Len() != 0 {
				t.Fatalf("unconfirmed result claimed success: error=%v output=%q", err, output.String())
			}
		})
	}
}
