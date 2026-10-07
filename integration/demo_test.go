package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tansrai/tansr-go/session"
)

func demoBinary(t *testing.T, name string) string {
	t.Helper()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	path := filepath.Join(t.TempDir(), name+suffix)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", path, "./examples/"+name)
	cmd.Dir = ".."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, output)
	}
	return path
}

func demoEnvironment(f fixture) []string {
	result := make([]string, 0, len(os.Environ())+2)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "TANSR_TOKEN", "TANSR_TOKEN_FILE", "TANSR_ARCHIVE_KEY":
			continue
		}
		result = append(result, value)
	}
	// Synthetic values only. The host's real model credentials are never read.
	return append(result, "TANSR_TOKEN="+f.Token, "TANSR_ARCHIVE_KEY="+strings.Repeat("ab", 32))
}

func TestRealServeChatAndArchiveDemos(t *testing.T) {
	f := startFixture(t, "archive")
	chat := demoBinary(t, "go-chat")
	archiver := demoBinary(t, "go-archive")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, chat, "-base", f.BaseURL, "-message", "GO-DEMO-CHAT", "-timeout", "30s")
	command.Env = demoEnvironment(f)
	output, err := command.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("go-archive-answer")) {
		t.Fatalf("go-chat: %v\n%s", err, output)
	}
	var id string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "session: ") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "session: "))
		}
	}
	if id == "" {
		t.Fatalf("go-chat did not publish resumable session: %s", output)
	}
	file := filepath.Join(physicalTempDir(t), "demo.archive")
	for i := 0; i < 2; i++ {
		command = exec.CommandContext(ctx, archiver, "-base", f.BaseURL, "-session", id, "-file", file)
		command.Env = demoEnvironment(f)
		output, err = command.CombinedOutput()
		if err != nil || !bytes.Contains(output, []byte("archive synchronized")) {
			t.Fatalf("go-archive run %d: %v\n%s", i+1, err, output)
		}
	}
	t.Log("go-chat command completed; go-archive command persisted and reopened the same real Serve archive")
}

func TestRealServeToolsDemo(t *testing.T) {
	f := startFixture(t, "execution-demo")
	binary := demoBinary(t, "go-tools")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-base", f.BaseURL, "-application", f.ApplicationScopeID, "-user", f.EndUserID,
		"-authorization-revision", f.AuthorizationRevision, "-executor", "go-executor", "-journal", t.TempDir())
	cmd.Env = demoEnvironment(f)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var exit error
	exited := false
	defer func() {
		cancel()
		if !exited {
			exit = <-waited
		}
		_ = exit
	}()
	var id string
	ready := false
	for !ready {
		select {
		case line, open := <-lines:
			if !open {
				exit = <-waited
				exited = true
				t.Fatalf("go-tools ended before readiness: %v\n%s", exit, stderr.String())
			}
			if strings.HasPrefix(line, "session: ") {
				id = strings.TrimSpace(strings.TrimPrefix(line, "session: "))
			}
			ready = strings.HasPrefix(line, "ready:")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if id == "" {
		t.Fatal("go-tools did not publish its original session")
	}
	sessions, err := session.New(clientFor(t, f))
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessions.Attach(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := s.Events(ctx, fmt.Sprint(s.Created().LastSeq))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = s.Send(ctx, "GO-TOOL", session.WriteOptions{IdempotencyKey: "go-tools-demo-message"}); err != nil {
		t.Fatal(err)
	}
	for {
		event, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == "server.permission.request" {
			var request struct {
				RequestID string `json:"requestId"`
				Digest    string `json:"digest"`
			}
			if err = json.Unmarshal(event.Raw, &request); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Permission(ctx, request.RequestID, request.Digest, "allow", session.WriteOptions{IdempotencyKey: "go-tools-demo-approval"}); err != nil {
				t.Fatal(err)
			}
		}
		if outcome, done := event.TurnOutcome(); done {
			if outcome.Status != session.OutcomeCompleted {
				t.Fatalf("demo tool outcome: %+v", outcome)
			}
			break
		}
	}
	history, err := s.History(ctx, 0, 20)
	if err != nil || !bytes.Contains(history, []byte("awaiting shipment")) || !bytes.Contains(history, []byte("go-tool-complete")) {
		t.Fatalf("demo result did not return through real Serve: %v\n%s", err, history)
	}
	t.Log("go-tools command registered, bound, executed DemoOrderStatus and returned its real receipt to the original Serve turn")
}
