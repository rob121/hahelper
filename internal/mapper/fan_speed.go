package mapper

import (
	"strings"

	haclient "github.com/mkelcik/go-ha-client/v2"
)

// fanSpeedHAP returns rotation speed 0–100 for HomeKit from HA fan state.
func fanSpeedHAP(se haclient.StateEntity) int {
	if se.State == "off" {
		return 0
	}
	if pct, ok := floatAttr(se, "percentage"); ok {
		return clampInt(int(pct), 0, 100)
	}
	if sp, ok := se.Attributes["speed"].(string); ok {
		switch strings.ToLower(sp) {
		case "low":
			return 33
		case "medium", "mid":
			return 66
		case "high":
			return 100
		}
	}
	return 100
}

func fanPercentageHA(speed int) int {
	return clampInt(speed, 0, 100)
}
