package executor

import "context"

// Frozen terminal-services-v1 reserved profile. The permission name is distinct
// from the registered transport tool name; it never enters the model tool table.
const MemoryPublicationToolName = "TansrTerminalMemoryPublication"
const MemoryPublicationDefinitionDigest = "8532a582d40d2d8993a80db59412a89671670ed7f994eaf9bf972bd544a111b0"

// MemoryPublicationHost must validate storage identity and the exact operation.
// Implementations may store bytes only; authorization remains a host responsibility.
type MemoryPublicationHost interface {
	Execute(context.Context, Operation, map[string]any) (any, error)
	RequiresEncryptedJournal() bool
}
