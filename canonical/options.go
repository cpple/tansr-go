package canonical

// Fixed constants of RFC-UAPI-1 §4.1. MaxBytes has no default: callers pass it explicitly.
const (
	// MaxDepth is the maximum nesting depth counted in ancestor containers (root = 0).
	MaxDepth = 32
	// MaxNodes is the maximum node count (root, containers and scalars; object keys are not nodes).
	MaxNodes = 100000
	// MaxSafeInteger is the largest representable integer (2^53 − 1).
	MaxSafeInteger = 1<<53 - 1
)

// Options bounds one encode / decode call.
//
// MaxBytes is required (≥ 1) and applies to the raw input bytes when decoding and to the accumulated
// output bytes when encoding. MaxDepth and MaxNodes may only tighten the fixed limits: 0 selects the
// fixed limit, values above [MaxDepth] / [MaxNodes] or below 0 (depth) / 1 (nodes) are rejected with
// invalid_options.
type Options struct {
	MaxBytes int
	MaxDepth int
	MaxNodes int
}

type limits struct {
	maxBytes int
	maxDepth int
	maxNodes int
}

func (o Options) normalize() (limits, *Error) {
	if o.MaxBytes < 1 {
		return limits{}, optionsError("MaxBytes must be ≥ 1 (got %d)", o.MaxBytes)
	}
	l := limits{maxBytes: o.MaxBytes, maxDepth: MaxDepth, maxNodes: MaxNodes}
	if o.MaxDepth != 0 {
		if o.MaxDepth < 0 || o.MaxDepth > MaxDepth {
			return limits{}, optionsError("MaxDepth must be in [0, %d] (got %d)", MaxDepth, o.MaxDepth)
		}
		l.maxDepth = o.MaxDepth
	}
	if o.MaxNodes != 0 {
		if o.MaxNodes < 1 || o.MaxNodes > MaxNodes {
			return limits{}, optionsError("MaxNodes must be in [1, %d] (got %d)", MaxNodes, o.MaxNodes)
		}
		l.maxNodes = o.MaxNodes
	}
	return l, nil
}

// IsValidKey reports whether key is a canonical object key: non-empty printable ASCII (0x21–0x7e).
func IsValidKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}
