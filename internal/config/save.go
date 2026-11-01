package config

import (
	"encoding/json"
	"os"
)

// Save writes the config struct back to path (used by admin UI).
func Save(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0600)
}
