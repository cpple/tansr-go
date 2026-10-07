package executor

import (
	"context"
	"errors"
	"runtime"
)

const Protocol = "sdk2-ext-v1"

var (
	ErrInvalid        = errors.New("executor: invalid contract value")
	ErrConflict       = errors.New("executor: operation or identity conflict")
	ErrLeaseExpired   = errors.New("executor: connection expired")
	ErrOutcomeUnknown = errors.New("executor: operation outcome unknown")
	ErrAlreadyRunning = errors.New("executor: runner is already running")
	ErrUnsupported    = errors.New("executor: unsupported operation")
)

type Scope struct {
	ApplicationScopeID    string `json:"applicationScopeId"`
	EndUserID             string `json:"endUserId"`
	AuthorizationRevision string `json:"authorizationRevision"`
}
type Platform struct {
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	Language       string `json:"language"`
	RuntimeVersion string `json:"runtimeVersion"`
	AdapterVersion string `json:"adapterVersion"`
}

// CurrentPlatform describes this Go process, not a remote UI or Serve host.
func CurrentPlatform() Platform {
	name := runtime.GOOS
	if name == "darwin" {
		name = "macos"
	}
	return Platform{Platform: name, Arch: runtime.GOARCH, Language: "go", RuntimeVersion: runtime.Version(), AdapterVersion: "go-executor-v1"}
}

type Workspace struct {
	WorkspaceID string `json:"workspaceId"`
	Revision    string `json:"revision"`
}
type ToolDefinition struct {
	Name             string `json:"name"`
	DefinitionDigest string `json:"definitionDigest"`
}
type Interpreter struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	HostShell string `json:"hostShell"`
}
type Registration struct {
	Protocol    string           `json:"protocol"`
	ExecutorID  string           `json:"executorId"`
	Platform    Platform         `json:"platform"`
	Workspaces  []Workspace      `json:"workspaces"`
	Operations  []string         `json:"operations"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Interpreter *Interpreter     `json:"interpreter,omitempty"`
}
type Connection struct {
	Protocol           string `json:"protocol"`
	ExecutorID         string `json:"executorId"`
	ConnectionID       string `json:"connectionId"`
	ConnectionRevision string `json:"connectionRevision"`
	ExpiresAt          string `json:"expiresAt"`
	HeartbeatAfterMS   int    `json:"heartbeatAfterMs"`
}
type Target struct {
	ExecutorID         string       `json:"executorId"`
	ConnectionID       string       `json:"connectionId"`
	ConnectionRevision string       `json:"connectionRevision"`
	WorkspaceID        string       `json:"workspaceId"`
	WorkspaceRevision  string       `json:"workspaceRevision"`
	Interpreter        *Interpreter `json:"interpreter,omitempty"`
}
type Binding struct {
	BindingID string `json:"bindingId"`
	Revision  string `json:"revision"`
	Target    Target `json:"target"`
}
type EffectiveTool struct {
	Name              string  `json:"name"`
	ExecutionKind     string  `json:"executionKind"`
	Available         bool    `json:"available"`
	UnavailableReason *string `json:"unavailableReason"`
}
type Capabilities struct {
	Protocol           string          `json:"protocol"`
	SessionID          string          `json:"sessionId"`
	Platform           *Platform       `json:"platform"`
	CapabilityRevision string          `json:"capabilityRevision"`
	EffectiveTools     []EffectiveTool `json:"effectiveTools"`
	Binding            *Binding        `json:"binding"`
}

// Resource is a validated ResourceRequest or ResourceResult. Receiving it never executes it.
type Resource struct {
	Operation string         `json:"operation"`
	Args      map[string]any `json:"args"`
}
type Operation struct {
	Protocol    string   `json:"protocol"`
	OperationID string   `json:"operationId"`
	SessionID   string   `json:"sessionId"`
	Scope       Scope    `json:"scope"`
	Binding     Binding  `json:"binding"`
	ToolName    string   `json:"toolName"`
	Request     Resource `json:"request"`
	Digest      string   `json:"digest"`
	ExpiresAt   string   `json:"expiresAt"`
}
type Batch struct {
	Protocol     string      `json:"protocol"`
	ExecutorID   string      `json:"executorId"`
	ConnectionID string      `json:"connectionId"`
	Operations   []Operation `json:"operations"`
}
type Receipt struct {
	Protocol     string    `json:"protocol"`
	ExecutorID   string    `json:"executorId"`
	ConnectionID string    `json:"connectionId"`
	OperationID  string    `json:"operationId"`
	Digest       string    `json:"digest"`
	Status       string    `json:"status"`
	Result       *Resource `json:"result"`
	ErrorCode    *string   `json:"errorCode"`
}
type Status struct {
	Protocol  string    `json:"protocol"`
	Operation Operation `json:"operation"`
	Status    string    `json:"status"`
	Receipt   *Receipt  `json:"receipt"`
}

// Tool is explicitly installed by the host. Handle receives ordinary JSON numbers as json.Number.
// It must obey context cancellation and return an SDK tool result receipt object (status/content).
// Ordinary errors and panics are unknown outcomes; use Rejected only if no effect occurred.
type Tool struct {
	DefinitionDigest string
	Handle           func(context.Context, map[string]any) (any, error)
}
type Rejected struct{ Code string }

func (e *Rejected) Error() string { return "executor: tool rejected: " + e.Code }

// ClaimResult is claimed, pending, or a previously durable receipt.
type ClaimResult struct {
	Claimed bool
	Receipt *Receipt
}
type Journal interface {
	Claim(context.Context, Operation) (ClaimResult, error)
	Complete(context.Context, Operation, Receipt) error
}
