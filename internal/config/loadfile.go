package config

import (
	"encoding/json"
	"os"
	"strings"
)

// LoadFile reads config from path using the same JSON shape as vhelp.
func LoadFile(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	c, err := parseConfigBytes(b)
	if err != nil {
		return Config{}, err
	}
	if err := c.resolveStorePath(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func parseConfigBytes(b []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if strings.Contains(string(b), `"HomeAssistant"`) {
		var legacy struct {
			HomeAssistant struct {
				URL   string `json:"URL"`
				Token string `json:"Token"`
			} `json:"HomeAssistant"`
		}
		if err := json.Unmarshal(b, &legacy); err == nil && legacy.HomeAssistant.URL != "" {
			if c.HomeAssistant.URL == "" {
				c.HomeAssistant.URL = legacy.HomeAssistant.URL
			}
			if c.HomeAssistant.Token == "" {
				c.HomeAssistant.Token = legacy.HomeAssistant.Token
			}
		}
	}
	c.applyDefaults()
	c.applyEnv()
	return c, nil
}

// MigrateLegacyFile fixes config written before json struct tags were added.
func MigrateLegacyFile(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(b), `"HomeAssistant"`) {
		return nil
	}
	c, err := parseConfigBytes(b)
	if err != nil {
		return err
	}
	if err := c.resolveStorePath(); err != nil {
		return err
	}
	return Save(path, c)
}
