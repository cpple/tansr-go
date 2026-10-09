package memorypublication

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/tansrai/tansr-go/canonical"
	"github.com/tansrai/tansr-go/executor"
	"github.com/tansrai/tansr-go/internal/wire"
)

type Host struct {
	store     Store
	identity  Identity
	encrypted bool
}

func NewHost(store Store, requireEncryption bool) (*Host, error) {
	if store == nil || !store.Capabilities().AtomicDurablePublication || requireEncryption && !store.Capabilities().EncryptedAtRest {
		return nil, executor.ErrUnsupported
	}
	return &Host{store, store.Identity(), requireEncryption || store.Capabilities().EncryptedAtRest}, nil
}
func (h *Host) RequiresEncryptedJournal() bool { return h.encrypted }

// Execute also validates direct trusted host calls; the runner independently
// verifies current binding, authorization, connection lease and operation journal.
func (h *Host) Execute(ctx context.Context, op executor.Operation, args map[string]any) (any, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	digest, e := executor.OperationDigest(op)
	if e != nil || digest != op.Digest || op.ToolName != "MemoryPublication" || op.Request.Operation != "tool.invoke" || op.Request.Args["name"] != executor.MemoryPublicationToolName || op.Request.Args["definitionDigest"] != executor.MemoryPublicationDefinitionDigest {
		return nil, &executor.Rejected{Code: "EACCES"}
	}
	deadline, e := time.Parse(time.RFC3339Nano, op.ExpiresAt)
	if e != nil || !time.Now().Before(deadline) {
		return nil, &executor.Rejected{Code: "EACCES"}
	}
	var actual any
	text, ok := op.Request.Args["argsJson"].(string)
	actual, e = canonical.Decode([]byte(text), canonical.Options{MaxBytes: 32768})
	if !ok || len(text) > 32768 || e != nil || !equal(actual, args) || op.Scope.ApplicationScopeID != h.identity.ApplicationScopeID || op.Scope.EndUserID != h.identity.EndUserID || h.store.Identity() != h.identity {
		return nil, &executor.Rejected{Code: "EACCES"}
	}
	r, e := normalize(Request(args))
	if e != nil {
		return nil, &executor.Rejected{Code: "EINVAL"}
	}
	if r["sourceId"] != h.identity.SourceID || r["sourceGeneration"] != h.identity.SourceGeneration || r["domainKey"] != h.identity.DomainKey {
		return nil, &executor.Rejected{Code: "EACCES"}
	}
	out, e := h.store.Execute(ctx, r, Owner{op.Scope, op.SessionID, op.Binding})
	if e != nil {
		var rejection AdapterError
		if errors.As(e, &rejection) {
			return map[string]any{"status": "error", "message": string(rejection)}, nil
		}
		return nil, e
	}
	if wire.Validate(Contract, "MemoryPublicationResponse", out) != nil {
		return nil, ErrIntegrity
	}
	for _, field := range []string{"action", "sourceId", "sourceGeneration", "domainKey"} {
		if !equal(r[field], out[field]) {
			return nil, ErrIntegrity
		}
	}
	if id, ok := r["transferId"]; ok {
		normalized, _ := canonicalBytes(out)
		v, e := canonical.Decode(normalized, canonical.Options{MaxBytes: 32768})
		if e != nil || v.(map[string]any)["transfer"].(map[string]any)["transferId"] != id {
			return nil, ErrIntegrity
		}
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return nil, e
	}
	return map[string]any{"status": "ok", "content": []any{map[string]any{"t": "text", "text": string(raw)}}}, nil
}
