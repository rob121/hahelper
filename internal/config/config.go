package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is loaded from config.json via github.com/rob121/vhelp.
type Config struct {
	HomeAssistant HomeAssistant `mapstructure:"home_assistant" json:"home_assistant"`
	Bridge        Bridge        `mapstructure:"bridge" json:"bridge"`
	UI            UI            `mapstructure:"ui" json:"ui"`
	HAP           HAP           `mapstructure:"hap" json:"hap"`
}

type HomeAssistant struct {
	URL         string `mapstructure:"url" json:"url"`
	Token       string `mapstructure:"token" json:"token,omitempty"`
	WSReconnect bool   `mapstructure:"ws_reconnect" json:"ws_reconnect"`
}

type Bridge struct {
	Name         string `mapstructure:"name" json:"name"`
	SerialNumber string `mapstructure:"serial_number" json:"serial_number"`
	Manufacturer string `mapstructure:"manufacturer" json:"manufacturer"`
	Model        string `mapstructure:"model" json:"model"`
	Firmware     string `mapstructure:"firmware" json:"firmware"`
	Pin          string `mapstructure:"pin" json:"pin"`
	SetupID      string `mapstructure:"setup_id" json:"setup_id"`
	// RoomNaming controls HomeKit accessory names (HAP cannot assign Apple Home rooms).
	// Values: area, entity_id, both, none.
	RoomNaming string `mapstructure:"room_naming" json:"room_naming"`
}

type UI struct {
	Listen string `mapstructure:"listen" json:"listen"`
}

type HAP struct {
	Pin        string `mapstructure:"pin" json:"pin"`
	StorePath  string `mapstructure:"store_path" json:"store_path"`
	Addr       string `mapstructure:"addr" json:"addr"`
	VerboseLog bool   `mapstructure:"verbose_log" json:"verbose_log"`
	// StatePollSeconds refreshes exposed entities from Home Assistant over REST and
	// pushes updates to HomeKit. Omitted or 0 uses 90; -1 disables polling.
	StatePollSeconds int `mapstructure:"state_poll_seconds" json:"state_poll_seconds"`
}

func Parse(v *viper.Viper) (Config, error) {
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return Config{}, err
	}
	c.applyDefaults()
	c.applyEnv()
	if err := c.resolveStorePath(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("HAHELPER_HA_URL"); v != "" {
		c.HomeAssistant.URL = v
	}
	if v := os.Getenv("HAHELPER_HA_TOKEN"); v != "" {
		c.HomeAssistant.Token = v
	}
}

func (c *Config) resolveStorePath() error {
	abs, err := filepath.Abs(c.HAP.StorePath)
	if err != nil {
		return err
	}
	c.HAP.StorePath = abs
	return nil
}

func (c *Config) applyDefaults() {
	if c.Bridge.Name == "" {
		c.Bridge.Name = "HA Helper"
	}
	if c.Bridge.Manufacturer == "" {
		c.Bridge.Manufacturer = "rob121"
	}
	if c.Bridge.Model == "" {
		c.Bridge.Model = "hahelper"
	}
	if c.Bridge.SerialNumber == "" {
		c.Bridge.SerialNumber = "hahelper-bridge"
	}
	if c.Bridge.Firmware == "" {
		c.Bridge.Firmware = "0.2.0"
	}
	if c.Bridge.Pin == "" {
		c.Bridge.Pin = "00102003"
	}
	if c.Bridge.SetupID == "" {
		c.Bridge.SetupID = "HAHP"
	}
	c.Bridge.SetupID = normalizeSetupID(c.Bridge.SetupID)
	if c.Bridge.RoomNaming == "" {
		c.Bridge.RoomNaming = "both"
	}

	if c.HAP.Pin == "" {
		c.HAP.Pin = c.Bridge.Pin
	}
	if c.HAP.StorePath == "" {
		c.HAP.StorePath = "./db"
	}
	if c.UI.Listen == "" {
		c.UI.Listen = "0.0.0.0:8080"
	}
	if !c.HomeAssistant.WSReconnect {
		c.HomeAssistant.WSReconnect = true
	}
}

func normalizeSetupID(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) < 4 {
		return "HAHP"
	}
	return out[:4]
}

func (c Config) HAConfigured() bool {
	return strings.TrimSpace(c.HomeAssistant.URL) != "" && strings.TrimSpace(c.HomeAssistant.Token) != ""
}

func (c Config) BridgeInfo() Bridge {
	return c.Bridge
}

func (c Config) EffectivePin() string {
	if c.HAP.Pin != "" {
		return c.HAP.Pin
	}
	return c.Bridge.Pin
}
