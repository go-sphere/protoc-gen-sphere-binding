package binding

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	// DefaultAutoRemoveJSON is the default behavior for JSON tags on fields bound
	// to a non-JSON location.
	DefaultAutoRemoveJSON = true
)

// Config controls struct-tag generation.
type Config struct {
	AutoRemoveJSON bool
	BindingAliases map[string][]string
}

// DefaultConfig returns the plugin's real defaults.
func DefaultConfig() *Config {
	return &Config{
		AutoRemoveJSON: DefaultAutoRemoveJSON,
		BindingAliases: map[string][]string{},
	}
}

// Validate checks configured binding tag keys and aliases.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is required")
	}
	for _, key := range slices.Sorted(maps.Keys(c.BindingAliases)) {
		if err := ValidateTagKey(key); err != nil {
			return fmt.Errorf("invalid binding alias key %q: %w", key, err)
		}
		for _, alias := range c.BindingAliases[key] {
			if err := ValidateTagKey(alias); err != nil {
				return fmt.Errorf("invalid binding alias %q for %q: %w", alias, key, err)
			}
		}
	}
	return nil
}

// ValidateTagKey reports whether key is valid in a Go struct tag.
func ValidateTagKey(key string) error {
	if key == "" {
		return errors.New("tag key cannot be empty")
	}
	if strings.ContainsAny(key, " \t\n\r`:\"") {
		return fmt.Errorf("tag key %q contains illegal characters", key)
	}
	return nil
}

// ParseBindingAliases parses and validates comma-separated key=value aliases.
func ParseBindingAliases(raw string) (map[string][]string, error) {
	aliases := make(map[string][]string)
	for alias := range strings.SplitSeq(raw, ",") {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}

		key, value, ok := strings.Cut(alias, "=")
		if !ok || strings.Contains(value, "=") {
			return nil, fmt.Errorf("invalid binding alias %q: expected 'key=value'", alias)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if err := ValidateTagKey(key); err != nil {
			return nil, fmt.Errorf("invalid binding alias %q: %w", alias, err)
		}
		if err := ValidateTagKey(value); err != nil {
			return nil, fmt.Errorf("invalid binding alias %q: %w", alias, err)
		}

		aliases[key] = append(aliases[key], value)
	}
	return aliases, nil
}
