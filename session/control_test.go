package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tansrai/tansr-go/api"
)

func TestCheckpointRoundTripKeepsOriginalBytes(t *testing.T) {
	original := []byte("{\n\"format\":\"tansr-checkpoint/1\",\"synthetic\":\"原字节\"\n}")
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testPath(api.OpDiscoverySessionCapabilities, map[string]string{"id": "s1"}):
			closure(w, api.StateEnabled)
		case testPath(api.OpSessionCheckpointExport, map[string]string{"id": "s1", "targetId": "c1"}):
			calls.Add(1)
			w.Header().Set(api.HeaderContract, api.Contract)
			w.Header().Set(api.HeaderManifestRevision, "7")
			w.Header().Set(api.HeaderDomain, "session")
			w.Header().Set(api.HeaderSchemaHash, "none")
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(original)
		case testPath(api.OpSessionCheckpointImport, map[string]string{"id": "s1"}):
			calls.Add(1)
			data, _ := io.ReadAll(r.Body)
			if !bytes.Equal(data, original) || r.Header.Get("Content-Type") != "application/octet-stream" || r.URL.Query().Get("label") != "本地快照" {
				t.Errorf("checkpoint bytes or request changed: %s", data)
			}
			response(w, "session", 201, map[string]any{"checkpointId": "c2", "sessionId": "s1", "createdAt": "2026-10-07T00:00:00Z", "trigger": "manual", "cwd": "/workspace", "messageCount": 5})
		default:
			t.Errorf("unexpected path %s", r.URL)
			response(w, "session", 500, map[string]any{})
		}
	})
	s := ref(c)
	data, err := s.ExportCheckpoint(context.Background(), "c1")
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("export failed %v", err)
	}
	item, err := s.ImportCheckpoint(context.Background(), data, "本地快照", WriteOptions{IdempotencyKey: "import-original"})
	if err != nil || item.CheckpointID != "c2" || len(item.Raw) == 0 || calls.Load() != 2 {
		t.Fatalf("import=%+v error=%v calls=%d", item, err, calls.Load())
	}
}

func TestManagementResultsPreserveRejectionAndIdentity(t *testing.T) {
	var writes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			closure(w, api.StateEnabled)
			return
		}
		switch writes.Add(1) {
		case 1:
			response(w, "session", 200, map[string]any{"status": "rejected", "reason": "not_configured"})
		case 2:
			response(w, "session", 200, map[string]any{"status": "restored", "checkpointId": "foreign", "fromMessages": 2, "toMessages": 1})
		default:
			t.Error("unexpected retry")
		}
	})
	s := ref(c)
	raw, err := s.Compact(context.Background(), CompactOptions{Checkpoint: map[string]string{"label": ""}}, WriteOptions{})
	if err != nil || !strings.Contains(string(raw), "rejected") {
		t.Fatalf("compaction rejection lost: %s %v", raw, err)
	}
	if _, err := s.Restore(context.Background(), "c1", true, WriteOptions{}); !errors.Is(err, &api.ClientError{Code: api.CodeInvalidResponse}) {
		t.Fatalf("foreign restore accepted: %v", err)
	}
}

func TestSpeechCarriesMediaSizedResultWithoutInventingSuccess(t *testing.T) {
	audio := strings.Repeat("A", 2*1024*1024+1)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			closure(w, api.StateEnabled)
			return
		}
		response(w, "session", 200, map[string]any{"model": "synthetic", "billedChars": 5, "audio": map[string]any{"b64": audio, "mime": "audio/wav", "format": "wav"}, "errorCode": "platform-deferred", "taskId": "task"})
	})
	raw, err := ref(c).Speak(context.Background(), SpeechRequest{Input: "hello"}, WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil || out["errorCode"] != "platform-deferred" || len(out["audio"].(map[string]any)["b64"].(string)) != len(audio) {
		t.Fatal("media bytes or deferred status were changed")
	}
}
