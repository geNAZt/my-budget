package db

import (
	"encoding/base64"
	"encoding/json"
)

// Marshal encodes a value to a base64-encoded JSON string
func Marshal(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Unmarshal decodes a base64-encoded JSON string or falls back to raw bytes.
func Unmarshal(data string, v interface{}) error {
	if data == "" {
		return nil
	}

	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		// Fallback to raw binary
		b = []byte(data)
	}

	// Try JSON first
	err = json.Unmarshal(b, v)
	if err == nil {
		return nil
	}

	return err
}
