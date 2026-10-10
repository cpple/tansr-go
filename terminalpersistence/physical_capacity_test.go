package terminalpersistence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"
)

func capacityBody(blocks int, seed byte) []byte {
	body := make([]byte, 0, blocks*MaxObject)
	for i := 0; i < blocks; i++ {
		sum := sha256.Sum256([]byte{seed, byte(i)})
		body = append(body, bytes.Repeat(sum[:], MaxObject/len(sum))...)
	}
	return body
}
func TestPhysicalCapacityRejectsBeginBeforeWriting(t *testing.T) {
	opts, owner := fixture(t)
	opts.MaxFileBytes = 1 << 20
	s := open(t, opts)
	p := plan(capacityBody(100, 1), nil, "too-large", nil, 0)
	before, _ := os.ReadFile(opts.Path)
	if _, err := s.Execute(context.Background(), p.begin, owner); !errors.Is(err, AdapterError("capacity_exceeded")) {
		t.Fatalf("known physical limit must reject begin: %v", err)
	}
	after, _ := os.ReadFile(opts.Path)
	if !bytes.Equal(before, after) || m(execute(t, s, query(p), owner)["transfer"])["status"] != "unknown" {
		t.Fatal("rejected admission changed media or created a ticket")
	}
}
func TestPhysicalCapacityReservationsSurviveConcurrentBeginAndReopen(t *testing.T) {
	opts, owner := fixture(t)
	opts.MaxFileBytes = 1 << 20
	s := open(t, opts)
	plans := []planned{plan(capacityBody(22, 1), nil, "first", nil, 0), plan(capacityBody(22, 2), nil, "second", nil, 0)}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, p := range plans {
		wg.Add(1)
		go func(p planned) {
			defer wg.Done()
			<-start
			_, err := s.Execute(context.Background(), p.begin, owner)
			results <- err
		}(p)
	}
	close(start)
	wg.Wait()
	for range plans {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	execute(t, s, plans[0].puts[0], owner)
	pending := execute(t, s, query(plans[0]), owner)
	s.Close()
	opts.Mode = "reopen"
	s = open(t, opts)
	if !same(pending, execute(t, s, query(plans[0]), owner)) {
		t.Fatal("reopen changed original pending fact")
	}
	third := plan(capacityBody(22, 3), nil, "third", nil, 0)
	before, _ := os.ReadFile(opts.Path)
	if _, err := s.Execute(context.Background(), third.begin, owner); !errors.Is(err, AdapterError("capacity_exceeded")) {
		t.Fatalf("existing reservations lost: %v", err)
	}
	after, _ := os.ReadFile(opts.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("capacity rejection wrote source")
	}
	// Both admitted object sets fit together before either terminal transition.
	for _, p := range plans {
		for _, put := range p.puts {
			execute(t, s, put, owner)
		}
	}
	root := m(execute(t, s, plans[0].commit, owner)["transfer"])["result"]
	if _, err := s.Execute(context.Background(), plans[1].commit, owner); !errors.Is(err, AdapterError("revision_conflict")) {
		t.Fatal(err)
	}
	if m(execute(t, s, query(plans[1]), owner)["transfer"])["status"] != "rejected" || !same(m(execute(t, s, query(plans[0]), owner)["transfer"])["result"], root) {
		t.Fatal("original terminal facts lost")
	}
}

func TestPhysicalCapacityKeepsPreviouslyAdmittedTicket(t *testing.T) {
	opts, owner := fixture(t)
	opts.MaxFileBytes = 1 << 20
	s := open(t, opts)
	p := plan(capacityBody(100, 9), nil, "legacy-pending", nil, 0)
	// Seed the unchanged old on-disk format through its former engine/save path.
	state := m(detached(s.state))
	who, err := ownerObject(owner)
	if err != nil {
		t.Fatal(err)
	}
	work := &engine{state: state}
	if _, err = work.run(p.begin, who, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.save(state, owner.Scope, context.Background()); err != nil {
		t.Fatal(err)
	}
	before := execute(t, s, query(p), owner)
	s.Close()
	opts.Mode = "reopen"
	s = open(t, opts)
	if !same(before, execute(t, s, query(p), owner)) {
		t.Fatal("old admission was changed")
	}
	execute(t, s, p.begin, owner) // idempotent original begin does not create a new admission
	execute(t, s, p.puts[0], owner)
	if m(m(execute(t, s, query(p), owner)["transfer"])["progress"])["receivedBytes"] == 0 {
		t.Fatal("old continuation lost")
	}
}
