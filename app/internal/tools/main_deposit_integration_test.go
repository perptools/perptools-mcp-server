package tools_test

import (
	"encoding/json"
)

// optEnvFloat reads a float from env or returns the fallback.
func optEnvFloat(key string, fallback float64) float64 {
	v := envOr(key, "")
	if v == "" {
		return fallback
	}
	var f float64
	if err := json.Unmarshal([]byte(v), &f); err != nil {
		return fallback
	}
	return f
}
