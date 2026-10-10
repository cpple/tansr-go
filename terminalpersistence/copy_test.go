package terminalpersistence

import (
	"bytes"
	"context"
	"errors"
	"github.com/tansrai/tansr-go/executor"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyPreservesWholeSnapshotAndNeverGrantsWrites(t *testing.T) {
	o, owner := fixture(t)
	s := open(t, o)
	p := plan([]byte{0xff, 0, 0x80}, []valueEntry{{hash([]byte("p")), hash([]byte("s")), []byte("receipt")}}, "committed", nil, 1)
	root := publish(t, s, p, owner)
	pending := plan([]byte("pending"), nil, "pending", root, 0)
	execute(t, s, pending.begin, owner)
	execute(t, s, pending.puts[0], owner)
	original, _ := os.ReadFile(o.Path)
	before := m(detached(s.state))
	targetPath := filepath.Join(filepath.Dir(o.Path), "candidate.bin")
	nextKey := bytes.Repeat([]byte{7}, 32)
	copied, e := s.CopyTo(context.Background(), CopyOptions{Path: targetPath, Key: nextKey})
	if e != nil {
		t.Fatal(e)
	}
	if !copied.CopyVerifiedCutoverPending() {
		t.Fatal("copy not restricted")
	}
	if !equal(execute(t, copied, query(p), owner), execute(t, s, query(p), owner)) || !equal(execute(t, copied, query(pending), owner), execute(t, s, query(pending), owner)) {
		t.Fatal("ticket changed")
	}
	for _, kind := range []string{"primary", "secondary"} {
		q := req("lookup")
		q["commitRoot"] = m(root)["commitRoot"]
		k := hash([]byte("p"))
		if kind == "secondary" {
			k = hash([]byte("s"))
		}
		q["key"] = obj{"kind": kind, "digest": k}
		if !equal(execute(t, copied, q, owner), execute(t, s, q, owner)) {
			t.Fatal("index changed")
		}
	}
	for _, r := range []Request{p.begin, p.puts[0], p.commit, pending.commit} {
		if _, e = copied.Execute(context.Background(), r, owner); e != AdapterError("read_only_copy") {
			t.Fatal("copy write", e)
		}
	}
	copied.Close()
	targetOptions := o
	targetOptions.Path = targetPath
	targetOptions.Mode = "reopen"
	targetOptions.Key = nextKey
	copied = open(t, targetOptions)
	if !copied.CopyVerifiedCutoverPending() {
		t.Fatal("reopen upgraded copy")
	}
	read := req("read")
	read["part"] = "body"
	read["commitRoot"] = m(root)["commitRoot"]
	read["offset"] = 0
	read["length"] = 3
	if !equal(execute(t, copied, read, owner), execute(t, s, read, owner)) {
		t.Fatal("body changed")
	}
	copied.Close()
	for _, bad := range []CopyOptions{{Path: targetPath, Key: nextKey}, {Path: targetPath + "same", Key: o.Key}} {
		if c, e := s.CopyTo(context.Background(), bad); e == nil {
			c.Close()
			t.Fatal("unsafe copy allowed")
		}
	}
	after, _ := os.ReadFile(o.Path)
	if !bytes.Equal(original, after) || !equal(before, s.state) {
		t.Fatal("source changed")
	}
	// A target remains read-only even after a second rekey; neither copy seals source.
	copied = open(t, targetOptions)
	twice, e := copied.CopyTo(context.Background(), CopyOptions{Path: targetPath + "2", Key: bytes.Repeat([]byte{8}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	defer twice.Close()
	if !twice.CopyVerifiedCutoverPending() {
		t.Fatal("rekey upgraded copy")
	}
	execute(t, s, pending.puts[1], owner)
}

func TestCopyFaultsKeepSourceAndPublishedTarget(t *testing.T) {
	for _, stage := range []string{"written", "file_synced", "before_replace", "replaced", "directory_synced"} {
		t.Run(stage, func(t *testing.T) {
			o, owner := fixture(t)
			s := open(t, o)
			p := plan([]byte("body"), nil, "ticket", nil, 0)
			publish(t, s, p, owner)
			original, _ := os.ReadFile(o.Path)
			target := filepath.Join(filepath.Dir(o.Path), "next.bin")
			key := bytes.Repeat([]byte{6}, 32)
			_, e := s.CopyTo(context.Background(), CopyOptions{Path: target, Key: key, CommitHook: func(at string) error {
				if at == stage {
					return errors.New("lost")
				}
				return nil
			}})
			if e == nil {
				t.Fatal("fault hidden")
			}
			after, _ := os.ReadFile(o.Path)
			if !bytes.Equal(original, after) {
				t.Fatal("source altered")
			}
			execute(t, s, query(p), owner)
			if stage == "replaced" || stage == "directory_synced" {
				if !errors.Is(e, ErrUnknown) {
					t.Fatal("durable copy failure not unknown", e)
				}
				o.Path = target
				o.Mode = "reopen"
				o.Key = key
				c := open(t, o)
				if !c.CopyVerifiedCutoverPending() || !equal(execute(t, c, query(p), owner), execute(t, s, query(p), owner)) {
					t.Fatal("published target lost")
				}
			} else if _, e = os.Stat(target); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("premature target", e)
			}
		})
	}
}

func TestCopyCancelAuthorityKeyAndCorruptionFences(t *testing.T) {
	for _, stage := range []string{"before_replace", "replaced"} {
		t.Run(stage, func(t *testing.T) {
			o, owner := fixture(t)
			scope := owner.Scope
			o.CurrentScope = func() (executor.Scope, error) { return scope, nil }
			s := open(t, o)
			original, _ := os.ReadFile(o.Path)
			_, e := s.CopyTo(context.Background(), CopyOptions{Path: o.Path + "next", Key: bytes.Repeat([]byte{6}, 32), CommitHook: func(at string) error {
				if at == stage {
					scope.AuthorizationRevision = "2"
				}
				return nil
			}})
			if e == nil {
				t.Fatal("revocation hidden")
			}
			scope = owner.Scope
			after, _ := os.ReadFile(o.Path)
			if !bytes.Equal(original, after) {
				t.Fatal("source changed")
			}
			execute(t, s, req("head"), owner)
		})
	}
	o, owner := fixture(t)
	s := open(t, o)
	original, _ := os.ReadFile(o.Path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.CopyTo(ctx, CopyOptions{Path: o.Path + "cancel", Key: bytes.Repeat([]byte{6}, 32)}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	// Exhaustion on the old key does not block a new-key read-only copy or reset source.
	s.encryptions = 1 << 20
	c, e := s.CopyTo(context.Background(), CopyOptions{Path: o.Path + "budget", Key: bytes.Repeat([]byte{6}, 32)})
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	if s.encryptions != 1<<20 {
		t.Fatal("source budget reset")
	}
	if _, e = s.Execute(context.Background(), plan([]byte("new"), nil, "new", nil, 0).begin, owner); e != AdapterError("capacity_exceeded") {
		t.Fatal("exhausted key wrote", e)
	}
	s.Close()
	o.Mode = "reopen"
	bad := o
	bad.Key = bytes.Repeat([]byte{1}, 32)
	if c, e = OpenFileStore(bad); e == nil {
		c.Close()
		t.Fatal("wrong key")
	}
	bad = o
	bad.Identity.SourceGeneration = "2"
	if c, e = OpenFileStore(bad); e == nil {
		c.Close()
		t.Fatal("wrong identity")
	}
	corrupt := append([]byte(nil), original...)
	corrupt[len(corrupt)-1] ^= 1
	if e = os.WriteFile(o.Path, corrupt, 0600); e != nil {
		t.Fatal(e)
	}
	if c, e = OpenFileStore(o); e == nil {
		c.Close()
		t.Fatal("corrupt source")
	}
	after, _ := os.ReadFile(o.Path)
	if !bytes.Equal(corrupt, after) {
		t.Fatal("bad original changed")
	}
}

func TestCopyRequiresReopenAfterUncertainSourceCommit(t *testing.T) {
	o, owner := fixture(t)
	s := open(t, o)
	p := plan([]byte("pending"), nil, "original", nil, 0)
	execute(t, s, p.begin, owner)
	for _, r := range p.puts {
		execute(t, s, r, owner)
	}
	s.options.CommitHook = func(stage string) error {
		if stage == "replaced" {
			return errors.New("lost source return")
		}
		return nil
	}
	if _, e := s.Execute(context.Background(), p.commit, owner); !errors.Is(e, ErrUnknown) {
		t.Fatal(e)
	}
	copyOptions := CopyOptions{Path: o.Path + "copy", Key: bytes.Repeat([]byte{7}, 32)}
	if _, e := s.CopyTo(context.Background(), copyOptions); !errors.Is(e, ErrUnknown) {
		t.Fatal("uncertain source copied", e)
	}
	s.Close()
	o.Mode = "reopen"
	s = open(t, o)
	c, e := s.CopyTo(context.Background(), copyOptions)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if m(execute(t, c, query(p), owner)["transfer"])["status"] != "committed" {
		t.Fatal("committed original lost")
	}
}
