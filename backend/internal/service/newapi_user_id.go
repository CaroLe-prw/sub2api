package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// NewAPIUserID preserves opaque IDs while accepting legacy numeric JSON IDs.
// Serialize as text to avoid losing precision in browsers or account extra JSON.
type NewAPIUserID string

func (id NewAPIUserID) valid() bool {
	if len(id) == 0 || len(id) > 256 || strings.TrimLeft(string(id), "0") == "" {
		return false
	}
	for i, ch := range id {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		if i > 0 && (ch == '-' || ch == '_') {
			continue
		}
		return false
	}
	return true
}

func (id *NewAPIUserID) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte("0")) {
		// Old disabled configurations used zero for an unconfigured UID.
		*id = ""
		return nil
	}
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return errors.New("invalid NewAPI UID")
		}
		value = strings.TrimSpace(value)
	} else {
		// Do not round numbers through float64 or accept fractional/exponent IDs.
		for _, ch := range data {
			if ch < '0' || ch > '9' {
				return errors.New("invalid NewAPI UID")
			}
		}
		value = string(data)
	}
	parsed := NewAPIUserID(value)
	if value != "" && !parsed.valid() {
		return errors.New("invalid NewAPI UID")
	}
	*id = parsed
	return nil
}
