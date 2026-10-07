package archive

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/cpple/tansr-go/api"
)

type MaterialRecord struct {
	RecordID    string        `json:"recordId"`
	Digest      string        `json:"digest"`
	Payload     ArtifactRef   `json:"payload"`
	Attachments []ArtifactRef `json:"attachments"`
}
type MaterialRequest struct {
	Protocol          string           `json:"protocol"`
	BindingID         string           `json:"bindingId"`
	MaterialRequestID string           `json:"materialRequestId"`
	Target            Target           `json:"target"`
	SourceID          string           `json:"sourceId"`
	SourceGeneration  string           `json:"sourceGeneration"`
	RequestedRecords  []MaterialRecord `json:"requestedRecords"`
	Purpose           string           `json:"purpose"`
	MaxBytes          int              `json:"maxBytes"`
	RemainingTTLMS    int              `json:"remainingTtlMs"`
	ChunkBytes        int              `json:"chunkBytes"`
}
type MaterialUploadRef struct {
	UploadID string `json:"uploadId"`
}
type MaterialRecordResponse struct {
	RecordID    string              `json:"recordId"`
	Digest      string              `json:"digest"`
	Payload     MaterialUploadRef   `json:"payload"`
	Attachments []MaterialUploadRef `json:"attachments"`
}
type MaterialResponse struct {
	Protocol          string                   `json:"protocol"`
	Request           RequestIdentity          `json:"request"`
	BindingID         string                   `json:"bindingId"`
	MaterialRequestID string                   `json:"materialRequestId"`
	Target            Target                   `json:"target"`
	SourceID          string                   `json:"sourceId"`
	SourceGeneration  string                   `json:"sourceGeneration"`
	Results           []MaterialRecordResponse `json:"results"`
}
type MaterialReceipt struct {
	Protocol          string   `json:"protocol"`
	BindingID         string   `json:"bindingId"`
	MaterialRequestID string   `json:"materialRequestId"`
	State             string   `json:"state"`
	Revision          string   `json:"revision"`
	AcceptedRecordIDs []string `json:"acceptedRecordIds"`
	Reason            string   `json:"reason,omitempty"`
}
type MaterialUploadChunk struct {
	Protocol          string `json:"protocol"`
	BindingID         string `json:"bindingId"`
	MaterialRequestID string `json:"materialRequestId"`
	Target            Target `json:"target"`
	SourceID          string `json:"sourceId"`
	SourceGeneration  string `json:"sourceGeneration"`
	ArtifactID        string `json:"artifactId"`
	Offset            int    `json:"offset"`
	Bytes             int    `json:"bytes"`
	ChunkSHA256       string `json:"chunkSha256"`
	Base64            string `json:"base64"`
}
type MaterialUploadStatus struct {
	Protocol          string      `json:"protocol"`
	BindingID         string      `json:"bindingId"`
	MaterialRequestID string      `json:"materialRequestId"`
	UploadID          string      `json:"uploadId"`
	Artifact          ArtifactRef `json:"artifact"`
	State             string      `json:"state"`
	ChunkBytes        int         `json:"chunkBytes"`
	ReceivedOffsets   []int       `json:"receivedOffsets"`
	ReceivedBytes     int         `json:"receivedBytes"`
	RemainingTTLMS    int         `json:"remainingTtlMs"`
}

// MaterialResult preserves the exact response for same-identity reconciliation
// when SubmitMaterials returns an uncertain error. Received is not core-consumed.
type MaterialResult struct {
	Response *MaterialResponse
	Receipt  *MaterialReceipt
}

func (c *Client) UploadMaterialChunk(ctx context.Context, input MaterialUploadChunk) (MaterialUploadStatus, error) {
	var value MaterialUploadStatus
	if err := validate("MaterialUploadChunkRequest", input); err != nil {
		return value, err
	}
	bytes, err := base64.StdEncoding.Strict().DecodeString(input.Base64)
	if err != nil || base64.StdEncoding.EncodeToString(bytes) != input.Base64 || len(bytes) != input.Bytes || digest(bytes) != input.ChunkSHA256 {
		return value, ErrIntegrity
	}
	err = c.call(ctx, api.OpMaterialUploadChunk, "MaterialUploadStatus", api.CallOptions{Params: map[string]string{"id": input.BindingID, "targetId": input.MaterialRequestID, "uploadId": input.ArtifactID}, Body: input}, 200, &value)
	if err != nil {
		return value, err
	}
	if err = verifyUpload(value, input.BindingID, input.MaterialRequestID, input.ArtifactID); err != nil {
		return value, err
	}
	if value.Artifact.SourceID != input.SourceID || input.Offset%value.ChunkBytes != 0 || !hasOffset(value.ReceivedOffsets, input.Offset) || input.Bytes != min(value.ChunkBytes, value.Artifact.Bytes-input.Offset) {
		return value, ErrIntegrity
	}
	if input.Offset == 0 && input.Bytes == value.Artifact.Bytes && value.Artifact.SHA256 != input.ChunkSHA256 {
		return value, ErrIntegrity
	}
	return value, nil
}
func hasOffset(offsets []int, value int) bool {
	for _, v := range offsets {
		if v == value {
			return true
		}
	}
	return false
}
func verifyUpload(value MaterialUploadStatus, bindingID, requestID, artifactID string) error {
	if err := validate("MaterialUploadStatus", value); err != nil {
		return err
	}
	if value.BindingID != bindingID || value.MaterialRequestID != requestID || value.Artifact.ArtifactID != artifactID || value.ChunkBytes <= 0 {
		return ErrIntegrity
	}
	previous := -1
	sum := 0
	for _, offset := range value.ReceivedOffsets {
		if offset <= previous || offset%value.ChunkBytes != 0 || offset >= value.Artifact.Bytes {
			return ErrIntegrity
		}
		previous = offset
		sum += min(value.ChunkBytes, value.Artifact.Bytes-offset)
	}
	if sum != value.ReceivedBytes || (value.Artifact.Bytes+value.ChunkBytes-1)/value.ChunkBytes > 16 || (value.State == "committed") != (value.ReceivedBytes == value.Artifact.Bytes) {
		return ErrIntegrity
	}
	return nil
}
func (c *Client) MaterialUploadStatus(ctx context.Context, bindingID, requestID, artifactID string) (MaterialUploadStatus, error) {
	var value MaterialUploadStatus
	for _, id := range []string{bindingID, requestID, artifactID} {
		if err := validate("Id", id); err != nil {
			return value, err
		}
	}
	err := c.call(ctx, api.OpMaterialUploadStatus, "MaterialUploadStatus", api.CallOptions{Params: map[string]string{"id": bindingID, "targetId": requestID, "uploadId": artifactID}, Query: map[string]string{"protocol": Protocol}}, 200, &value)
	if err == nil {
		err = verifyUpload(value, bindingID, requestID, artifactID)
	}
	return value, err
}
func (c *Client) SubmitMaterials(ctx context.Context, input MaterialResponse) (MaterialReceipt, error) {
	var value MaterialReceipt
	if err := validate("MaterialResponseRequest", input); err != nil {
		return value, err
	}
	seen := map[string]bool{}
	for _, r := range input.Results {
		if seen[r.RecordID] {
			return value, ErrIntegrity
		}
		seen[r.RecordID] = true
	}
	err := c.call(ctx, api.OpMaterialResponseSubmit, "MaterialReceipt", api.CallOptions{Params: map[string]string{"id": input.BindingID}, Body: input, IdempotencyKey: input.Request.RequestID}, 202, &value)
	if err != nil {
		return value, err
	}
	if value.BindingID != input.BindingID || value.MaterialRequestID != input.MaterialRequestID || value.State != "received" || value.Revision == "0" || len(value.AcceptedRecordIDs) != len(seen) {
		return value, ErrIntegrity
	}
	for _, id := range value.AcceptedRecordIDs {
		if !seen[id] {
			return value, ErrIntegrity
		}
		delete(seen, id)
	}
	return value, nil
}
func (c *Client) MaterialStatus(ctx context.Context, bindingID, requestID string) (MaterialReceipt, error) {
	var value MaterialReceipt
	for _, id := range []string{bindingID, requestID} {
		if err := validate("Id", id); err != nil {
			return value, err
		}
	}
	err := c.call(ctx, api.OpMaterialStatus, "MaterialReceipt", api.CallOptions{Params: map[string]string{"id": bindingID, "targetId": requestID}, Query: map[string]string{"protocol": Protocol}}, 200, &value)
	if err == nil && (value.BindingID != bindingID || value.MaterialRequestID != requestID || value.State == "pending" && (value.Revision != "0" || len(value.AcceptedRecordIDs) != 0)) {
		err = ErrIntegrity
	}
	return value, err
}

// RespondMaterials serves only the exact current material request from the
// explicitly selected store. The caller fixes and persists the request identity;
// retrying must reuse it. No URL, path, partial-record fallback or new authority
// can be supplied by the request. The context is bounded by the request's TTL.
func RespondMaterials(ctx context.Context, client *Client, store Store, request MaterialRequest, identity RequestIdentity) (MaterialResult, error) {
	var result MaterialResult
	if err := validate("MaterialRequest", request); err != nil {
		return result, err
	}
	if err := validate("RequestIdentity", identity); err != nil {
		return result, err
	}
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
	expected := store.Identity()
	if request.BindingID != expected.BindingID || request.SourceID != expected.SourceID || request.SourceGeneration != expected.SourceGeneration || request.Target.SessionID != expected.SessionID || request.Target.Generations != expected.Generations {
		return result, ErrIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.RemainingTTLMS)*time.Millisecond)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ids := make([]string, 0, len(request.RequestedRecords))
	for _, r := range request.RequestedRecords {
		ids = append(ids, r.RecordID)
	}
	saved, err := store.ReadRecordsByID(ids)
	if err != nil {
		return result, err
	}
	if len(saved) != len(ids) {
		return result, ErrIntegrity
	}
	records := map[string]Record{}
	for _, r := range saved {
		if _, ok := records[r.RecordID]; ok {
			return result, ErrIntegrity
		}
		if err := verifyRecord(r, 262144); err != nil {
			return result, err
		}
		records[r.RecordID] = r
	}
	seen := map[string]bool{}
	refs := map[string]ArtifactRef{}
	total := 0
	for _, asked := range request.RequestedRecords {
		r, ok := records[asked.RecordID]
		if !ok || seen[asked.RecordID] || r.RecordDigest != asked.Digest || r.Payload != asked.Payload || !equal(r.Attachments, asked.Attachments) || r.Target.SessionID != request.Target.SessionID || r.Target.Generations != request.Target.Generations {
			return result, ErrIntegrity
		}
		seen[asked.RecordID] = true
		for _, ref := range append([]ArtifactRef{r.Payload}, r.Attachments...) {
			if ref.SourceID != expected.SourceID {
				return result, ErrIntegrity
			}
			if prior, ok := refs[ref.ArtifactID]; ok {
				if prior != ref {
					return result, ErrIntegrity
				}
			} else {
				if ref.Bytes > min(request.MaxBytes, limits.MaxBatchBytes)-total {
					return result, ErrCapacity
				}
				total += ref.Bytes
				refs[ref.ArtifactID] = ref
			}
		}
	}
	if total > request.MaxBytes || total > limits.MaxBatchBytes {
		return result, ErrCapacity
	}
	// Verify all selected bytes before any upload; a missing object never becomes
	// a partial success. The core still owns context selection and consumption.
	bodies := map[string][]byte{}
	for id, ref := range refs {
		body, err := store.Body(ref)
		if err != nil {
			return result, err
		}
		fixed := append([]byte{}, body...)
		if len(fixed) != ref.Bytes || digest(fixed) != ref.SHA256 {
			return result, ErrIntegrity
		}
		bodies[id] = fixed
	}
	for _, asked := range request.RequestedRecords {
		if domainDigest("tansr.sdk2.payload.v1", bodies[asked.Payload.ArtifactID]) != records[asked.RecordID].PayloadDigest {
			return result, ErrIntegrity
		}
	}
	uploads := map[string]MaterialUploadRef{}
	for _, asked := range request.RequestedRecords {
		for _, ref := range append([]ArtifactRef{asked.Payload}, asked.Attachments...) {
			if _, ok := uploads[ref.ArtifactID]; ok {
				continue
			}
			body := bodies[ref.ArtifactID]
			var last MaterialUploadStatus
			for offset := 0; offset < len(body); offset += request.ChunkBytes {
				if err := store.CheckAccess(); err != nil {
					return result, err
				}
				chunk := body[offset:min(offset+request.ChunkBytes, len(body))]
				value, err := client.UploadMaterialChunk(ctx, MaterialUploadChunk{Protocol: Protocol, BindingID: request.BindingID, MaterialRequestID: request.MaterialRequestID, Target: request.Target, SourceID: request.SourceID, SourceGeneration: request.SourceGeneration, ArtifactID: ref.ArtifactID, Offset: offset, Bytes: len(chunk), ChunkSHA256: digest(chunk), Base64: base64.StdEncoding.EncodeToString(chunk)})
				if err != nil {
					return result, err
				}
				if value.Artifact != ref || value.ChunkBytes != request.ChunkBytes {
					return result, ErrIntegrity
				}
				last = value
			}
			if last.State != "committed" {
				value, err := client.MaterialUploadStatus(ctx, request.BindingID, request.MaterialRequestID, ref.ArtifactID)
				if err != nil {
					return result, err
				}
				last = value
			}
			if last.State != "committed" || last.Artifact != ref {
				return result, ErrIntegrity
			}
			uploads[ref.ArtifactID] = MaterialUploadRef{last.UploadID}
		}
	}
	response := MaterialResponse{Protocol: Protocol, Request: identity, BindingID: request.BindingID, MaterialRequestID: request.MaterialRequestID, Target: request.Target, SourceID: request.SourceID, SourceGeneration: request.SourceGeneration, Results: []MaterialRecordResponse{}}
	for _, asked := range request.RequestedRecords {
		row := MaterialRecordResponse{RecordID: asked.RecordID, Digest: asked.Digest, Payload: uploads[asked.Payload.ArtifactID], Attachments: []MaterialUploadRef{}}
		for _, ref := range asked.Attachments {
			row.Attachments = append(row.Attachments, uploads[ref.ArtifactID])
		}
		response.Results = append(response.Results, row)
	}
	result.Response = &response
	if err := store.CheckAccess(); err != nil {
		return result, err
	}
	receipt, err := client.SubmitMaterials(ctx, response)
	if err != nil {
		return result, err
	}
	result.Receipt = &receipt
	return result, nil
}
