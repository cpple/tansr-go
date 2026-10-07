// Package contract exposes the frozen protocol schemas shipped with this SDK.
// These files are copied from Serve's contract source and checked by the lock
// manifest; client code must not fetch or infer schemas at runtime.
package contract

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed *.schema.json
var schemas embed.FS

// ReadSchema returns a copy of a frozen schema. name is the contract name without
// the .schema.json suffix, for example "sdk2-ext-v1".
func ReadSchema(name string) ([]byte, error) {
	if name == "" || strings.ContainsAny(name, "/\\.") {
		return nil, fmt.Errorf("contract: invalid schema name")
	}
	data, err := schemas.ReadFile(name + ".schema.json")
	if err != nil {
		return nil, fmt.Errorf("contract: schema %q is not frozen: %w", name, err)
	}
	return data, nil
}
