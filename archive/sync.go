package archive

import (
	"context"
	"fmt"
)

// SyncOnce recovers an original pending ACK or receives exactly one new page.
// It never advances an SSE cursor or continues indefinitely to catch up.
func SyncOnce(ctx context.Context, client *Client, store Store, requestID string) (SyncResult, error) {
	var result SyncResult
	if store == nil {
		return result, ErrClosed
	}
	if err := store.CheckAccess(); err != nil {
		return result, err
	}
	limits, err := defaultLimits(store.StorageLimits())
	if err != nil {
		return result, err
	}
	pending, err := store.Pending()
	if err != nil {
		return result, err
	}
	if pending != nil {
		if err = store.CheckAccess(); err != nil {
			return result, err
		}
		receipt, err := client.Acknowledge(ctx, *pending)
		if err != nil {
			return result, err
		}
		if err = store.Confirm(receipt); err != nil {
			return result, err
		}
		result.Recovered = true
		result.Receipt = &receipt
		return result, nil
	}
	identity := store.Identity()
	binding, err := client.Binding(ctx, identity.BindingID)
	if err != nil {
		return result, err
	}
	status, err := client.Status(ctx, identity.BindingID)
	if err != nil {
		return result, err
	}
	current, err := IdentityFrom(binding, status)
	if err != nil {
		return result, err
	}
	if current != identity {
		return result, ErrIntegrity
	}
	head, err := store.Head()
	if err != nil {
		return result, err
	}
	var after *string
	if head != nil {
		after = &head.Sequence
	}
	page, err := client.ReadRecords(ctx, binding, after)
	if err != nil {
		return result, err
	}
	if len(page.Records) == 0 {
		result.Complete = page.Complete
		return result, nil
	}
	if binding.OperationEpoch == nil {
		return result, fmt.Errorf("archive: no active operation epoch")
	}
	request := RequestIdentity{requestID, binding.OperationEpoch.ID}
	if err = validate("RequestIdentity", request); err != nil {
		return result, err
	}
	refs := map[string]ArtifactRef{}
	batch := 0
	for _, record := range page.Records {
		metadata, _ := canon(record)
		if len(metadata) > limits.MaxBatchBytes-batch {
			return result, ErrCapacity
		}
		batch += len(metadata)
		for _, ref := range append([]ArtifactRef{record.Payload}, record.Attachments...) {
			if _, ok := refs[ref.ArtifactID]; !ok {
				if ref.Bytes > limits.MaxBatchBytes-batch {
					return result, ErrCapacity
				}
				batch += ref.Bytes
			}
			refs[ref.ArtifactID] = ref
		}
	}
	if len(refs) > limits.MaxArtifacts || batch > limits.MaxBatchBytes {
		return result, ErrCapacity
	}
	bodies := map[string][]byte{}
	for id, ref := range refs {
		body, err := client.ReadArtifact(ctx, binding, ref)
		if err != nil {
			return result, err
		}
		bodies[id] = body
	}
	ack, err := store.Receive(binding, status, page, bodies, request)
	if err != nil {
		return result, err
	}
	if err = store.CheckAccess(); err != nil {
		return result, err
	}
	receipt, err := client.Acknowledge(ctx, ack)
	if err != nil {
		return result, err
	}
	if err = store.Confirm(receipt); err != nil {
		return result, err
	}
	return SyncResult{Records: len(page.Records), Complete: page.Complete, Receipt: &receipt}, nil
}
