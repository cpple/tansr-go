// Package memorypublication stores the frozen terminal MemoryPublication profile.
// It performs byte storage and CAS only; Serve retains all memory business decisions.
package memorypublication

import (
	"context"
	"errors"

	"github.com/tansrai/tansr-go/executor"
)

const Contract = "terminal-services-v1"
const MaxBody = 4 << 20
const MaxChunk = 12288

var ErrIntegrity = errors.New("memorypublication: integrity check failed")
var ErrClosed = errors.New("memorypublication: store closed")
var ErrUnknown = errors.New("memorypublication: durable outcome unknown; reopen original store")

// AdapterError is a known storage rejection, never an uncertain IO failure.
type AdapterError string

func (e AdapterError) Error() string { return string(e) }

type Identity struct {
	ApplicationScopeID string `json:"applicationScopeId"`
	EndUserID          string `json:"endUserId"`
	SourceID           string `json:"sourceId"`
	SourceGeneration   string `json:"sourceGeneration"`
	DomainKey          string `json:"domainKey"`
}
type Owner struct {
	Scope     executor.Scope   `json:"scope"`
	SessionID string           `json:"sessionId"`
	Binding   executor.Binding `json:"binding"`
}

// Request and Response retain exactly the frozen action-dependent fields.
// Execute validates them against the bundled, locked schema.
type Request map[string]any
type Response map[string]any
type Capabilities struct{ AtomicDurablePublication, EncryptedAtRest bool }
type Store interface {
	Identity() Identity
	Capabilities() Capabilities
	Execute(context.Context, Request, Owner) (Response, error)
}
type Limits struct {
	MaxTransfers    int `json:"maxTransfers"`
	MaxStagingBytes int `json:"maxStagingBytes"`
	MaxFileBytes    int `json:"maxFileBytes"`
}
type Capacity struct {
	Limits                        Limits
	StoredTransfers, StagingBytes int
}
type Options struct {
	Path string
	// Mode must be create or reopen. Missing recovery media are never recreated.
	Mode string
	// Key is supplied by a host key store, as for archive.StoreOptions; never persisted.
	Key      []byte
	Identity Identity
	Limits   Limits
	// CurrentScope consults current authorization, never saved metadata. Callbacks
	// must not reenter the store. It is checked before and after every operation.
	CurrentScope func() (executor.Scope, error)
	// AuthorizeRecovery permits query only after host verification of old-owner
	// revocation and current-owner authorization. It never permits writes.
	AuthorizeRecovery func(Identity, string, Owner, Owner) bool
}
