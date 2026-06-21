package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed schemas/replay.schema.json schemas/focus.schema.json
var schemaFS embed.FS

func schemaPayload(name string) ([]byte, error) {
	var filename string
	switch name {
	case "replay":
		filename = "schemas/replay.schema.json"
	case "focus":
		filename = "schemas/focus.schema.json"
	default:
		return nil, fmt.Errorf("unknown schema name: %s", name)
	}
	raw, err := schemaFS.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	// Ensure output is valid JSON before writing it to callers.
	if !json.Valid(raw) {
		return nil, fmt.Errorf("embedded %s schema is not valid JSON", name)
	}
	return raw, nil
}
