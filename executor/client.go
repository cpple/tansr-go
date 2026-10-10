package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/canonical"
	"github.com/tansrai/tansr-go/internal/wire"
)

const controlBytes = 262144

type Client struct {
	api   *api.Client
	scope Scope
}

func NewClient(client *api.Client, scope Scope) (*Client, error) {
	if client == nil {
		return nil, ErrInvalid
	}
	if err := validate("Scope", scope); err != nil {
		return nil, err
	}
	return &Client{api: client, scope: scope}, nil
}
func (c *Client) Scope() Scope     { return c.scope }
func (c *Client) API() *api.Client { return c.api }

func validate(name string, value any) error {
	if err := wire.Validate("sdk2-ext-v1", name, value); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalid, name, err)
	}
	return nil
}
func (c *Client) call(ctx context.Context, op, schema string, opts api.CallOptions, out any) error {
	opDef, ok := api.Lookup(op)
	if !ok {
		return ErrInvalid
	}
	if opts.Body != nil {
		name := strings.SplitN(opDef.Request, "#", 2)
		if len(name) != 2 {
			return ErrInvalid
		}
		if err := validate(name[1], opts.Body); err != nil {
			return err
		}
	}
	opts.MaxResponseBytes = controlBytes
	result, err := c.api.Call(ctx, op, opts)
	if err != nil {
		return err
	}
	expectedStatus := 200
	if op == api.OpExecutorRegister {
		expectedStatus = 201
	}
	if result.Status != expectedStatus {
		return fmt.Errorf("%w: unexpected HTTP status %d", ErrInvalid, result.Status)
	}
	if _, err = wire.Decode("sdk2-ext-v1", schema, result.Body); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalid, schema, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Body))
	decoder.UseNumber()
	if err = decoder.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

func validateRegistration(in Registration) error {
	if err := validate("ExecutorRegistrationRequest", in); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, workspace := range in.Workspaces {
		if seen[workspace.WorkspaceID] {
			return ErrInvalid
		}
		seen[workspace.WorkspaceID] = true
	}
	seen = map[string]bool{}
	for _, tool := range in.Tools {
		if seen[tool.Name] {
			return ErrInvalid
		}
		seen[tool.Name] = true
	}
	hasTools := false
	for _, op := range in.Operations {
		if op == "tool.invoke" {
			hasTools = true
		}
	}
	if hasTools != (len(in.Tools) > 0) {
		return ErrInvalid
	}
	return nil
}
func (c *Client) Register(ctx context.Context, in Registration) (Connection, error) {
	var out Connection
	if err := validateRegistration(in); err != nil {
		return out, err
	}
	err := c.call(ctx, api.OpExecutorRegister, "ExecutorConnection", api.CallOptions{Body: in}, &out)
	if err == nil && out.ExecutorID != in.ExecutorID {
		err = ErrConflict
	}
	if err == nil {
		err = liveConnection(out)
	}
	return out, err
}
func liveConnection(in Connection) error {
	if err := validate("ExecutorConnection", in); err != nil {
		return err
	}
	expires, err := time.Parse(time.RFC3339Nano, in.ExpiresAt)
	if err != nil || !expires.After(time.Now()) {
		return ErrLeaseExpired
	}
	return nil
}
func (c *Client) Heartbeat(ctx context.Context, in Connection) (Connection, error) {
	var out Connection
	if err := liveConnection(in); err != nil {
		return out, err
	}
	err := c.call(ctx, api.OpExecutorHeartbeat, "ExecutorConnection", api.CallOptions{Params: map[string]string{"id": in.ExecutorID}, Body: map[string]any{"protocol": Protocol, "executorId": in.ExecutorID, "connectionId": in.ConnectionID}}, &out)
	if err == nil && (out.ExecutorID != in.ExecutorID || out.ConnectionID != in.ConnectionID) {
		err = ErrConflict
	}
	if err == nil {
		err = liveConnection(out)
	}
	return out, err
}
func (c *Client) Poll(ctx context.Context, connection Connection) (Batch, error) {
	var out Batch
	if err := liveConnection(connection); err != nil {
		return out, err
	}
	err := c.call(ctx, api.OpExecutorOperationsPoll, "ExecutionBatch", api.CallOptions{Params: map[string]string{"id": connection.ExecutorID}, Query: map[string]string{"connectionId": connection.ConnectionID}}, &out)
	if err != nil {
		return out, err
	}
	if out.ExecutorID != connection.ExecutorID || out.ConnectionID != connection.ConnectionID {
		return out, ErrConflict
	}
	seen := map[string]bool{}
	for _, operation := range out.Operations {
		if seen[operation.OperationID] {
			return out, ErrConflict
		}
		seen[operation.OperationID] = true
		if err = validateOperation(operation); err != nil {
			return out, err
		}
		target := operation.Binding.Target
		if operation.Scope != c.scope || target.ExecutorID != connection.ExecutorID || target.ConnectionID != connection.ConnectionID || target.ConnectionRevision != connection.ConnectionRevision {
			return out, ErrConflict
		}
	}
	return out, nil
}

func (c *Client) Initialize(ctx context.Context, sessionID string, platform Platform, requestedTools []string, closureID string) (Capabilities, error) {
	body := map[string]any{"protocol": Protocol, "sessionId": sessionID, "platform": platform}
	if requestedTools != nil {
		body["requestedTools"] = requestedTools
	}
	var out Capabilities
	seen := map[string]bool{}
	for _, name := range requestedTools {
		if seen[name] {
			return out, ErrInvalid
		}
		seen[name] = true
	}
	err := c.call(ctx, api.OpExecutionInitialize, "SessionExecutionCapabilities", api.CallOptions{Params: map[string]string{"id": sessionID}, Body: body, ClosureID: closureID}, &out)
	if err == nil && (out.SessionID != sessionID || out.Platform == nil || *out.Platform != platform) {
		err = ErrConflict
	}
	if err == nil {
		err = validateCapabilities(out)
	}
	return out, err
}
func validateCapabilities(out Capabilities) error {
	seen := map[string]bool{}
	for _, tool := range out.EffectiveTools {
		if seen[tool.Name] {
			return ErrConflict
		}
		seen[tool.Name] = true
	}
	return nil
}

// ExecutionCapabilities reads the current revision for a subsequent explicit binding decision.
// It does not reinitialize the session or retry a rejected bind automatically.
func (c *Client) ExecutionCapabilities(ctx context.Context, sessionID string) (Capabilities, error) {
	var out Capabilities
	err := c.call(ctx, api.OpExecutionCapabilities, "SessionExecutionCapabilities", api.CallOptions{Params: map[string]string{"id": sessionID}}, &out)
	if err != nil {
		return out, err
	}
	if out.SessionID != sessionID {
		return out, ErrConflict
	}
	return out, validateCapabilities(out)
}
func (c *Client) Bind(ctx context.Context, sessionID string, connection Connection, workspace Workspace, capabilityRevision, closureID string) (Capabilities, error) {
	var out Capabilities
	if err := liveConnection(connection); err != nil {
		return out, err
	}
	body := map[string]any{"protocol": Protocol, "sessionId": sessionID, "executorId": connection.ExecutorID, "connectionId": connection.ConnectionID, "workspaceId": workspace.WorkspaceID, "expectedCapabilityRevision": capabilityRevision}
	err := c.call(ctx, api.OpExecutionBindingCreate, "SessionExecutionCapabilities", api.CallOptions{Params: map[string]string{"id": sessionID}, Body: body, ClosureID: closureID}, &out)
	if err != nil {
		return out, err
	}
	if out.SessionID != sessionID || out.Binding == nil {
		return out, ErrConflict
	}
	target := out.Binding.Target
	if target.ExecutorID != connection.ExecutorID || target.ConnectionID != connection.ConnectionID || target.ConnectionRevision != connection.ConnectionRevision || target.WorkspaceID != workspace.WorkspaceID || target.WorkspaceRevision != workspace.Revision {
		return out, ErrConflict
	}
	return out, validateCapabilities(out)
}
func (c *Client) Status(ctx context.Context, sessionID, operationID string) (Status, error) {
	var out Status
	err := c.call(ctx, api.OpExecutionStatus, "ExecutionStatus", api.CallOptions{Params: map[string]string{"id": sessionID, "targetId": operationID}}, &out)
	if err != nil {
		return out, err
	}
	if out.Operation.SessionID != sessionID || out.Operation.OperationID != operationID {
		return out, ErrConflict
	}
	return out, c.validateStatus(out)
}

// ExecutorStatus uses the restricted terminal.execution.state read for an executor-only ticket.
// The controller must first negotiate the terminal binding for this session. This call does not
// negotiate, elevate the ticket, change the operation or fall back to a controller endpoint.
func (c *Client) ExecutorStatus(ctx context.Context, session TerminalSessionReference, connection Connection, operation Operation) (Status, error) {
	var out Status
	if err := validateOperation(operation); err != nil {
		return out, err
	}
	if err := liveConnection(connection); err != nil {
		return out, err
	}
	if err := wire.Validate("terminal-services-v1", "SessionReference", session); err != nil {
		return out, fmt.Errorf("%w: terminal session", ErrInvalid)
	}
	if session.SessionID != operation.SessionID || operation.Scope.ApplicationScopeID != c.scope.ApplicationScopeID || operation.Scope.EndUserID != c.scope.EndUserID {
		return out, ErrConflict
	}
	target := operation.Binding.Target
	if target.ExecutorID != connection.ExecutorID || target.ConnectionID != connection.ConnectionID || target.ConnectionRevision != connection.ConnectionRevision {
		return out, ErrConflict
	}
	result, err := c.api.Call(ctx, api.OpTerminalExecutionState, api.CallOptions{
		Params:           map[string]string{"id": connection.ExecutorID, "targetId": operation.OperationID},
		Query:            map[string]string{"sessionContract": session.SessionContract, "sessionId": session.SessionID, "requestDigest": operation.Digest, "connectionId": connection.ConnectionID},
		MaxResponseBytes: controlBytes,
	})
	if err != nil {
		return out, err
	}
	if result.Status != 200 {
		return out, ErrInvalid
	}
	if _, err = wire.Decode("terminal-services-v1", "ExecutionState", result.Body); err != nil {
		return out, fmt.Errorf("%w: terminal state: %v", ErrInvalid, err)
	}
	var response struct {
		Contract  string                   `json:"contract"`
		Session   TerminalSessionReference `json:"session"`
		Execution Status                   `json:"execution"`
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Body))
	decoder.UseNumber()
	if err = decoder.Decode(&response); err != nil {
		return out, ErrInvalid
	}
	if response.Session != session || !equal(response.Execution.Operation, operation) {
		return out, ErrConflict
	}
	return response.Execution, c.validateStatus(response.Execution)
}
func (c *Client) Submit(ctx context.Context, operation Operation, receipt Receipt) (Status, error) {
	var out Status
	if operation.Scope.ApplicationScopeID != c.scope.ApplicationScopeID || operation.Scope.EndUserID != c.scope.EndUserID {
		return out, ErrConflict
	}
	if err := validateReceipt(operation, receipt); err != nil {
		return out, err
	}
	err := c.call(ctx, api.OpExecutorReceiptSubmit, "ExecutionStatus", api.CallOptions{Params: map[string]string{"id": receipt.ExecutorID}, Body: receipt}, &out)
	if err != nil {
		return out, err
	}
	if !equal(out.Operation, operation) || out.Receipt == nil || !equal(*out.Receipt, receipt) || out.Status != receipt.Status {
		return out, ErrConflict
	}
	return out, c.validateStatus(out)
}
func (c *Client) validateStatus(out Status) error {
	if err := validateOperation(out.Operation); err != nil {
		return err
	}
	if out.Operation.Scope.ApplicationScopeID != c.scope.ApplicationScopeID || out.Operation.Scope.EndUserID != c.scope.EndUserID {
		return ErrConflict
	}
	if out.Status == "pending" || out.Status == "unknown" && out.Receipt == nil {
		if out.Receipt != nil {
			return ErrConflict
		}
		return nil
	}
	if out.Receipt == nil || out.Receipt.Status != out.Status {
		return ErrConflict
	}
	return validateReceipt(out.Operation, *out.Receipt)
}

// OperationDigest computes the frozen execution digest, ignoring the current Digest field.
func OperationDigest(operation Operation) (string, error) {
	operation.Digest = strings.Repeat("0", 64)
	if err := validate("ExecutionOperation", operation); err != nil {
		return "", err
	}
	encoded, err := canonical.Encode(operation, canonical.Options{MaxBytes: 1048576})
	if err != nil {
		return "", err
	}
	value, err := canonical.Decode(encoded, canonical.Options{MaxBytes: 1048576})
	if err != nil {
		return "", err
	}
	payload := value.(map[string]any)
	delete(payload, "digest")
	encoded, err = canonical.Encode(payload, canonical.Options{MaxBytes: 1048576})
	if err != nil {
		return "", err
	}
	digest, err := canonical.DomainDigest("tansr.sdk2.execution.v1", encoded)
	return canonical.Hex(digest), err
}
func validateOperation(operation Operation) error {
	digest, err := OperationDigest(operation)
	if err != nil {
		return err
	}
	if operation.Digest != digest {
		return ErrConflict
	}
	switch operation.Request.Operation {
	case "tool.invoke":
		// Reserved Shell/MemoryPublication profiles need their dedicated adapters. This package's
		// business runner deliberately cannot turn those names into device execution authority.
		if operation.Request.Args["name"] == TerminalPersistenceToolName {
			if operation.ToolName != "MemoryPublication" || operation.Request.Args["definitionDigest"] != TerminalPersistenceDefinitionDigest {
				return ErrInvalid
			}
			text, ok := operation.Request.Args["argsJson"].(string)
			if !ok || len(text) > 32768 {
				return ErrInvalid
			}
			args, err := canonical.Decode([]byte(text), canonical.Options{MaxBytes: 32768})
			if err != nil {
				return err
			}
			return wire.Validate("terminal-persistence-v1", "Request", args)
		}
		if operation.Request.Args["name"] == MemoryPublicationToolName {
			if operation.ToolName != "MemoryPublication" || operation.Request.Args["definitionDigest"] != MemoryPublicationDefinitionDigest {
				return ErrUnsupported
			}
			args, e := toolObject(operation.Request.Args["argsJson"].(string))
			if e != nil {
				return e
			}
			return wire.Validate("terminal-services-v1", "MemoryPublicationRequest", args)
		}
		if operation.ToolName == "MemoryPublication" || operation.Request.Args["name"] != operation.ToolName {
			return ErrUnsupported
		}
		_, err = toolObject(operation.Request.Args["argsJson"].(string))
		return err
	case "process.exec":
		if operation.Binding.Target.Interpreter == nil || !equal(operation.Binding.Target.Interpreter, operation.Request.Args["interpreter"]) {
			return ErrConflict
		}
	}
	return nil
}
func equal(a, b any) bool {
	x, e1 := canonical.Encode(a, canonical.Options{MaxBytes: controlBytes})
	y, e2 := canonical.Encode(b, canonical.Options{MaxBytes: controlBytes})
	return e1 == nil && e2 == nil && bytes.Equal(x, y)
}
func validateReceipt(operation Operation, receipt Receipt) error {
	if err := validateOperation(operation); err != nil {
		return err
	}
	if err := validate("ExecutionReceiptRequest", receipt); err != nil {
		return err
	}
	if receipt.OperationID != operation.OperationID || receipt.Digest != operation.Digest || receipt.ExecutorID != operation.Binding.Target.ExecutorID || receipt.ConnectionID != operation.Binding.Target.ConnectionID {
		return ErrConflict
	}
	if receipt.Status != "completed" {
		if receipt.Result != nil || receipt.ErrorCode == nil {
			return ErrInvalid
		}
		return nil
	}
	if receipt.Result == nil || receipt.ErrorCode != nil || receipt.Result.Operation != operation.Request.Operation {
		return ErrInvalid
	}
	result, asked := receipt.Result.Args, operation.Request.Args
	switch receipt.Result.Operation {
	case "tool.invoke":
		return verifyToolResult(result["resultJson"].(string))
	case "fs.read":
		data, err := decodeBase64(result["bytesBase64"].(string))
		if err != nil || len(data) > 65536 || int64(len(data)) > integer(asked["length"]) {
			return ErrInvalid
		}
	case "fs.write":
		data, err := decodeBase64(asked["bytesBase64"].(string))
		if err != nil || canonical.Hex(sha256.Sum256(data)) != result["hash"] {
			return ErrInvalid
		}
	case "fs.list":
		seen := map[string]bool{}
		encoded, _ := json.Marshal(result["entries"])
		var entries []struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(encoded, &entries)
		for _, entry := range entries {
			if seen[entry.Name] || strings.ContainsAny(entry.Name, "/\\\x00") || entry.Name == "." || entry.Name == ".." {
				return ErrInvalid
			}
			seen[entry.Name] = true
		}
	case "process.exec":
		if len(result["stdout"].(string))+len(result["stderr"].(string)) > int(integer(asked["maxOutputBytes"])) {
			return ErrInvalid
		}
		if result["exitCode"] != nil {
			if _, err := strconv.ParseInt(result["exitCode"].(string), 10, 32); err != nil {
				return ErrInvalid
			}
		}
	}
	return nil
}
func integer(value any) int64 {
	switch n := value.(type) {
	case json.Number:
		r, _ := n.Int64()
		return r
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return -1
}
func decodeBase64(text string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil || base64.StdEncoding.EncodeToString(data) != text {
		return nil, ErrInvalid
	}
	return data, nil
}

// clone prevents a host authorization callback from mutating the operation later used for IO.
func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var out T
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	_ = d.Decode(&out)
	return out
}
