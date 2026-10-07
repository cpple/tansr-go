package session

import (
	"context"
	"encoding/json"
	"math"
	"unicode/utf16"

	"github.com/cpple/tansr-go/api"
)

const mediaBytes = 32 * 1024 * 1024

// Compact uses Serve's existing context manager. The result preserves rejected
// and failed outcomes, including any pre-compaction checkpoint. It never repeats
// a model call automatically.
func (s *Session) Compact(ctx context.Context, input CompactOptions, opts WriteOptions) (json.RawMessage, error) {
	if utf16Length(input.Instructions) > 4096 {
		return nil, invalidBody("compaction instructions exceed 4096 characters")
	}
	if input.Checkpoint != nil {
		if _, ok := input.Checkpoint.(bool); !ok {
			value, ok := input.Checkpoint.(map[string]string)
			_, hasLabel := value["label"]
			if !ok || len(value) > 1 || len(value) == 1 && !hasLabel {
				return nil, invalidBody("checkpoint must be bool or a map with an optional label")
			}
			if err := checkpointLabel(value["label"]); err != nil {
				return nil, err
			}
		}
	}
	res, err := s.write(ctx, api.OpSessionCompact, nil, input, opts)
	raw, err := rawResult(res, err, 200)
	if err != nil {
		return nil, err
	}
	var value struct {
		Status       string  `json:"status"`
		Reason       string  `json:"reason"`
		Message      string  `json:"message"`
		CompactionID string  `json:"compactionId"`
		RemovedRange []int64 `json:"removedRange"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return nil, invalidResponse("invalid compaction result")
	}
	switch value.Status {
	case "compacted":
		if value.CompactionID == "" || len(value.RemovedRange) != 2 || !safeCount(value.RemovedRange[0]) || !safeCount(value.RemovedRange[1]) || value.RemovedRange[0] > value.RemovedRange[1] {
			return nil, invalidResponse("invalid compacted range")
		}
	case "rejected":
		if value.Reason != "empty_history" && value.Reason != "not_configured" && value.Reason != "hook_blocked" {
			return nil, invalidResponse("invalid compaction rejection")
		}
	case "failed":
		if value.Reason == "" {
			return nil, invalidResponse("invalid compaction failure")
		}
	default:
		return nil, invalidResponse("unrecognized compaction result")
	}
	return raw, nil
}

func (s *Session) Checkpoints(ctx context.Context) ([]Checkpoint, error) {
	res, err := s.client.transport.Call(ctx, api.OpSessionCheckpointList, api.CallOptions{Params: s.params()})
	if err != nil {
		return nil, err
	}
	var body struct {
		Checkpoints []json.RawMessage `json:"checkpoints"`
	}
	if err := decode(res, 200, &body); err != nil {
		return nil, err
	}
	if body.Checkpoints == nil {
		return nil, invalidResponse("checkpoint list must contain an array")
	}
	result := make([]Checkpoint, 0, len(body.Checkpoints))
	for _, raw := range body.Checkpoints {
		item, err := s.readCheckpoint(raw, "")
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Session) Checkpoint(ctx context.Context, label string, opts WriteOptions) (Checkpoint, error) {
	if err := checkpointLabel(label); err != nil {
		return Checkpoint{}, err
	}
	res, err := s.write(ctx, api.OpSessionCheckpointCreate, nil, map[string]string{"label": label}, opts)
	raw, err := rawResult(res, err, 201)
	if err != nil {
		return Checkpoint{}, err
	}
	return s.readCheckpoint(raw, "")
}

// Restore leaves the checkpoint body and context reconstruction to Serve. A
// requested pre-restore checkpoint is controlled by the checkpoint flag.
func (s *Session) Restore(ctx context.Context, checkpointID string, checkpoint bool, opts WriteOptions) (json.RawMessage, error) {
	res, err := s.write(ctx, api.OpSessionCheckpointRestore, map[string]string{"targetId": checkpointID}, map[string]bool{"checkpoint": checkpoint}, opts)
	raw, err := rawResult(res, err, 200)
	if err != nil {
		return nil, err
	}
	var body struct {
		Status       string `json:"status"`
		CheckpointID string `json:"checkpointId"`
		FromMessages *int64 `json:"fromMessages"`
		ToMessages   *int64 `json:"toMessages"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Status != "restored" || body.CheckpointID != checkpointID || body.FromMessages == nil || body.ToMessages == nil || !safeCount(*body.FromMessages) || !safeCount(*body.ToMessages) {
		return nil, invalidResponse("restore receipt does not match the requested checkpoint")
	}
	return raw, nil
}

func (s *Session) DeleteCheckpoint(ctx context.Context, checkpointID string, opts WriteOptions) error {
	res, err := s.write(ctx, api.OpSessionCheckpointDelete, map[string]string{"targetId": checkpointID}, nil, opts)
	if err != nil {
		return err
	}
	if res.Status != 204 || len(res.Body) != 0 {
		return invalidResponse("checkpoint deletion did not return an empty 204")
	}
	return nil
}

func (s *Session) ExportCheckpoint(ctx context.Context, checkpointID string) ([]byte, error) {
	params := s.params()
	params["targetId"] = checkpointID
	res, err := s.client.transport.Call(ctx, api.OpSessionCheckpointExport, api.CallOptions{Params: params, MaxResponseBytes: mediaBytes})
	if err != nil {
		return nil, err
	}
	if res.Status != 200 || res.ContentType != "application/octet-stream" || len(res.Body) == 0 {
		return nil, invalidResponse("checkpoint export must be nonempty octet-stream bytes")
	}
	return res.Body, nil
}

func (s *Session) ImportCheckpoint(ctx context.Context, data []byte, label string, opts WriteOptions) (Checkpoint, error) {
	if len(data) == 0 || len(data) > mediaBytes {
		return Checkpoint{}, invalidBody("checkpoint must contain 1 to 32 MiB of original export bytes")
	}
	if err := checkpointLabel(label); err != nil {
		return Checkpoint{}, err
	}
	query := map[string]string{}
	if label != "" {
		query["label"] = label
	}
	res, err := s.writeQuery(ctx, api.OpSessionCheckpointImport, nil, query, append([]byte(nil), data...), opts)
	raw, err := rawResult(res, err, 201)
	if err != nil {
		return Checkpoint{}, err
	}
	return s.readCheckpoint(raw, "")
}

func (s *Session) SetCwd(ctx context.Context, cwd string, opts WriteOptions) (json.RawMessage, error) {
	if cwd == "" {
		return nil, invalidBody("cwd must not be empty")
	}
	res, err := s.write(ctx, api.OpSessionCwdSet, nil, map[string]string{"cwd": cwd}, opts)
	// Serve's configured policy resolves and validates the workspace. This is
	// never treated as permission to access the client's or Serve's filesystem.
	return rawResult(res, err, 200)
}

// Transcribe requests an audio draft. It does not silently send the transcript
// as a user message or repeat a billable transcription.
func (s *Session) Transcribe(ctx context.Context, input TranscriptionRequest, opts WriteOptions) (json.RawMessage, error) {
	if input.Audio == "" {
		return nil, invalidBody("audio is required")
	}
	res, err := s.write(ctx, api.OpSessionAudioTranscribe, nil, input, opts)
	return rawResult(res, err, 200)
}

// Speak preserves the platform result (including errorCode/taskId); a 200 does
// not guarantee an audio artifact. Inspect the returned object before playback.
func (s *Session) Speak(ctx context.Context, input SpeechRequest, opts WriteOptions) (json.RawMessage, error) {
	if input.Input == "" || input.Format != "" && input.Format != "mp3" && input.Format != "wav" || input.Speed != nil && (math.IsNaN(*input.Speed) || math.IsInf(*input.Speed, 0)) {
		return nil, invalidBody("invalid speech input, format or speed")
	}
	res, err := s.write(ctx, api.OpSessionAudioSpeak, nil, input, opts)
	return rawResult(res, err, 200)
}

func (s *Session) readCheckpoint(raw json.RawMessage, checkpointID string) (Checkpoint, error) {
	var item Checkpoint
	var fields struct {
		MessageCount *int64 `json:"messageCount"`
	}
	if json.Unmarshal(raw, &item) != nil || json.Unmarshal(raw, &fields) != nil || item.CheckpointID == "" || item.SessionID != s.ID() || fields.MessageCount == nil || !safeCount(*fields.MessageCount) || checkpointID != "" && item.CheckpointID != checkpointID {
		return Checkpoint{}, invalidResponse("checkpoint identity or count is invalid")
	}
	item.Raw = append(json.RawMessage(nil), raw...)
	return item, nil
}

func checkpointLabel(label string) error {
	if utf16Length(label) > 120 {
		return invalidBody("checkpoint label exceeds 120 characters")
	}
	return nil
}

// Serve's string caps count UTF-16 units, matching its JavaScript contracts.
func utf16Length(value string) int { return len(utf16.Encode([]rune(value))) }
