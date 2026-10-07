package archive

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cpple/tansr-go/api"
	"github.com/cpple/tansr-go/internal/wire"
)

const recoverySchema = "sdk2-archive-recovery-v1"
const rebaseRequestBytes = 263168
const rebaseResponseBytes = 528384

type AckRebaseRequest struct {
	Protocol  string          `json:"protocol"`
	BindingID string          `json:"bindingId"`
	Previous  Ack             `json:"previous"`
	Request   RequestIdentity `json:"request"`
}
type AckRebaseReceipt struct {
	Protocol  string          `json:"protocol"`
	BindingID string          `json:"bindingId"`
	Previous  Ack             `json:"previous"`
	Request   RequestIdentity `json:"request"`
	Next      Ack             `json:"next"`
	Receipt   MutationReceipt `json:"receipt"`
}

// RecoveryStore is an optional extension for explicit recovery of a stale ACK.
// PrepareRebase atomically saves the immutable previous ACK and recovery identity
// with enough capacity to confirm its result. A retry must use that saved intent.
type RecoveryStore interface {
	Store
	PendingRebase() (*AckRebaseRequest, error)
	PrepareRebase(RequestIdentity) (AckRebaseRequest, error)
	ConfirmRebase(AckRebaseReceipt) error
}

type rebaseEntry struct {
	Intent          AckRebaseRequest  `json:"intent"`
	Result          *AckRebaseReceipt `json:"result"`
	OriginalReceipt *MutationReceipt  `json:"originalReceipt"`
}

func validateRebase(input AckRebaseRequest) error {
	if err := wire.Validate(recoverySchema, "AckRebaseRequest", input); err != nil {
		return err
	}
	if input.BindingID != input.Previous.BindingID || input.Request == input.Previous.Request || input.Request.OperationEpoch != input.Previous.Request.OperationEpoch || !validCoverage(input.Previous.Coverage) {
		return ErrIntegrity
	}
	previous, err := canon(input.Previous)
	if err != nil {
		return err
	}
	raw, err := canon(input)
	if err != nil {
		return err
	}
	if len(previous) > 262144 || len(raw) > rebaseRequestBytes {
		return ErrCapacity
	}
	return nil
}
func verifyRebaseResult(input AckRebaseRequest, result AckRebaseReceipt) error {
	if err := validateRebase(input); err != nil {
		return err
	}
	if err := wire.Validate(recoverySchema, "AckRebaseReceipt", result); err != nil {
		return err
	}
	if result.Protocol != input.Protocol || result.BindingID != input.BindingID || !equal(result.Previous, input.Previous) || result.Request != input.Request || result.Next.Request != input.Request || seq(result.Next.ExpectedRevision) <= seq(input.Previous.ExpectedRevision) {
		return ErrReceipt
	}
	prior := clone(result.Next)
	prior.Request = input.Previous.Request
	prior.ExpectedRevision = input.Previous.ExpectedRevision
	if !equal(prior, input.Previous) {
		return ErrReceipt
	}
	raw, err := canon(result.Next)
	if err != nil {
		return err
	}
	if len(raw) > 262144 {
		return ErrCapacity
	}
	return nil
}

// RebaseAcknowledgement sends an already durable recovery intent. Only the
// server may choose the next revision under its original binding's idle lease.
// The store must verify the scope-framed receipt with ConfirmRebase afterwards.
func (c *Client) RebaseAcknowledgement(ctx context.Context, input AckRebaseRequest) (AckRebaseReceipt, error) {
	var result AckRebaseReceipt
	if err := validateRebase(input); err != nil {
		return result, err
	}
	if c == nil || c.api == nil {
		return result, errors.New("archive: API client is required")
	}
	raw, err := c.api.Call(ctx, api.OpArchiveAckRebase, api.CallOptions{Params: map[string]string{"id": input.BindingID}, Body: input, IdempotencyKey: input.Request.RequestID, MaxResponseBytes: rebaseResponseBytes})
	if err != nil {
		return result, err
	}
	if raw.Status != 200 || raw.ContentType != "application/json" {
		return result, ErrIntegrity
	}
	if _, err = wire.Decode(recoverySchema, "AckRebaseReceipt", raw.Body); err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw.Body, &result); err != nil {
		return result, err
	}
	return result, verifyRebaseResult(input, result)
}

func reservedRebaseIdentity(state storeState, request RequestIdentity) bool {
	if state.LastReceipt != nil && state.LastReceipt.Request == request {
		return true
	}
	for _, row := range state.Rebases {
		if row.Intent.Request == request || row.Intent.Previous.Request == request {
			return true
		}
	}
	return false
}
func (s *FileStore) PendingRebase() (*AckRebaseRequest, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	for _, row := range s.state.Rebases {
		if row.Result == nil && row.OriginalReceipt == nil {
			if err := s.authorize(); err != nil {
				return nil, err
			}
			return clone(&row.Intent), nil
		}
	}
	return nil, s.authorize()
}

// PrepareRebase explicitly enables recovery storage. Its first successful save
// atomically upgrades v1 to v2, preserving all bytes, pending ACK and coverage.
// Older SDKs fail closed on the new format; opening or syncing v1 never migrates it.
func (s *FileStore) PrepareRebase(request RequestIdentity) (AckRebaseRequest, error) {
	var zero AckRebaseRequest
	if err := s.enter(); err != nil {
		return zero, err
	}
	defer s.mu.Unlock()
	if err := validate("RequestIdentity", request); err != nil {
		return zero, err
	}
	for _, row := range s.state.Rebases {
		if row.Result == nil && row.OriginalReceipt == nil {
			if row.Intent.Request != request {
				return zero, ErrPendingAck
			}
			if err := s.authorize(); err != nil {
				return zero, err
			}
			return clone(row.Intent), nil
		}
	}
	if s.state.Pending == nil || reservedRebaseIdentity(s.state, request) {
		return zero, ErrReceipt
	}
	intent := AckRebaseRequest{Protocol: Protocol, BindingID: s.identity.BindingID, Previous: clone(*s.state.Pending), Request: request}
	if err := validateRebase(intent); err != nil {
		return zero, err
	}
	next := clone(s.state)
	next.Format = recoveryStoreFormat
	next.Rebases = append(next.Rebases, rebaseEntry{Intent: intent})
	total, err := logicalBytes(next)
	if err != nil {
		return zero, err
	}
	if total > s.limits.MaxStoredBytes {
		return zero, ErrCapacity
	}
	if err = s.save(next); err != nil {
		return zero, err
	}
	s.state = next
	return clone(intent), nil
}

// ConfirmRebase records the old-to-next mapping and completed receipt in the
// same durable transaction that advances coverage; it never rewrites old ACKs.
func (s *FileStore) ConfirmRebase(result AckRebaseReceipt) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.mu.Unlock()
	for index, row := range s.state.Rebases {
		if row.Intent.Request != result.Request {
			continue
		}
		if err := verifyRebaseResult(row.Intent, result); err != nil {
			return err
		}
		if err := verifyReceipt(s.identity, result.Next, result.Receipt); err != nil {
			return err
		}
		if row.Result != nil {
			if equal(*row.Result, result) {
				return s.authorize()
			}
			return ErrReceipt
		}
		if row.OriginalReceipt != nil || s.state.Pending == nil || !equal(*s.state.Pending, row.Intent.Previous) {
			return ErrReceipt
		}
		next := clone(s.state)
		copy := clone(result)
		next.Rebases[index].Result = &copy
		next.Coverage = &copy.Next.Coverage
		next.LastReceipt = &copy.Receipt
		next.Pending = nil
		if err := s.save(next); err != nil {
			return err
		}
		s.state = next
		return nil
	}
	return ErrReceipt
}

func (s *FileStore) validateRebases() error {
	if s.state.Format == storeFormat && len(s.state.Rebases) != 0 {
		return ErrIntegrity
	}
	seen := map[RequestIdentity]bool{}
	unresolved := 0
	for _, row := range s.state.Rebases {
		input := row.Intent
		ack := input.Previous
		if validateRebase(input) != nil || seen[input.Request] || seen[ack.Request] || ack.BindingID != s.identity.BindingID || ack.SourceID != s.identity.SourceID || ack.SourceGeneration != s.identity.SourceGeneration || ack.Generations != s.identity.Generations || seq(ack.Coverage.ThroughSequence) > int64(len(s.state.Records)) || s.state.Records[seq(ack.Coverage.ThroughSequence)-1].RecordDigest != ack.Coverage.HeadDigest {
			return ErrIntegrity
		}
		seen[input.Request], seen[ack.Request] = true, true
		if row.Result != nil {
			if row.OriginalReceipt != nil || verifyRebaseResult(input, *row.Result) != nil || verifyReceipt(s.identity, row.Result.Next, row.Result.Receipt) != nil {
				return ErrIntegrity
			}
		} else if row.OriginalReceipt != nil {
			if verifyReceipt(s.identity, ack, *row.OriginalReceipt) != nil {
				return ErrIntegrity
			}
		} else {
			unresolved++
			if s.state.Pending == nil || !equal(*s.state.Pending, ack) {
				return ErrIntegrity
			}
		}
		if row.Result != nil || row.OriginalReceipt != nil {
			if s.state.Coverage == nil || seq(ack.Coverage.ThroughSequence) > seq(s.state.Coverage.ThroughSequence) {
				return ErrIntegrity
			}
		}
	}
	if unresolved > 1 {
		return ErrIntegrity
	}
	return nil
}

func staleAckRevision(err error) bool {
	var failure *api.APIError
	return errors.As(err, &failure) && failure.Code == api.CodePreconditionFailed && failure.Detail.Reason == api.ReasonIfMatchStale && (failure.Detail.DomainCode == "binding_conflict" || failure.Detail.DomainCode == "stale_revision")
}

// RecoverPending is explicit recovery, not a generic mutation retry. It first
// replays the original ACK. Only a definitive stale If-Match response permits
// preparing requestID in the original epoch. An existing recovery intent always
// wins over requestID, including after a lost response or process restart.
// Busy/expired/revoked responses retain the intent for a later same-request call.
// requestID must be a new, globally unique application request identity; a v1
// archive does not contain the complete history of earlier server operations.
func RecoverPending(ctx context.Context, client *Client, store RecoveryStore, requestID string) (SyncResult, error) {
	var result SyncResult
	if store == nil {
		return result, ErrClosed
	}
	if err := store.CheckAccess(); err != nil {
		return result, err
	}
	intent, err := store.PendingRebase()
	if err != nil {
		return result, err
	}
	if intent == nil {
		pending, err := store.Pending()
		if err != nil {
			return result, err
		}
		if pending == nil {
			return result, ErrPendingAck
		}
		receipt, err := client.Acknowledge(ctx, *pending)
		if err == nil {
			if err = store.Confirm(receipt); err != nil {
				return result, err
			}
			return SyncResult{Recovered: true, Receipt: &receipt}, nil
		}
		if !staleAckRevision(err) {
			return result, err
		}
		prepared, err := store.PrepareRebase(RequestIdentity{RequestID: requestID, OperationEpoch: pending.Request.OperationEpoch})
		if err != nil {
			return result, err
		}
		intent = &prepared
	}
	return resumeRebase(ctx, client, store, *intent)
}
func resumeRebase(ctx context.Context, client *Client, store RecoveryStore, intent AckRebaseRequest) (SyncResult, error) {
	var result SyncResult
	if err := store.CheckAccess(); err != nil {
		return result, err
	}
	rebased, err := client.RebaseAcknowledgement(ctx, intent)
	if err != nil {
		var failure *api.APIError
		if errors.As(err, &failure) && failure.Code == api.CodeConflict && failure.Detail.DomainCode == "request_id_conflict" {
			// Another original replay may have committed after recovery preparation.
			receipt, queryErr := client.Operation(ctx, intent.BindingID, "archive-ack", intent.Previous.Request)
			if queryErr == nil {
				if confirmErr := store.Confirm(receipt); confirmErr != nil {
					return result, confirmErr
				}
				return SyncResult{Recovered: true, Receipt: &receipt}, nil
			}
		}
		return result, err
	}
	if err = store.ConfirmRebase(rebased); err != nil {
		return result, err
	}
	return SyncResult{Recovered: true, Receipt: &rebased.Receipt}, nil
}
