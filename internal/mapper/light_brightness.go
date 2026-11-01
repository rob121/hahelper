package mapper

import (
	haclient "github.com/mkelcik/go-ha-client/v2"
)

// lightLevelHAP returns HomeKit brightness 0–100 from a HA light/switch state.
func lightLevelHAP(se haclient.StateEntity) (int, bool) {
	if pct, ok := floatAttr(se, "brightness_pct"); ok {
		return clampInt(int(pct), 0, 100), true
	}
	if b, ok := floatAttr(se, "brightness"); ok {
		return clampInt(int(brightnessHAP(b)), 0, 100), true
	}
	if se.State == "on" {
		return 100, true
	}
	return 0, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func supportsLightLevel(se haclient.StateEntity) bool {
	if _, ok := floatAttr(se, "brightness"); ok {
		return true
	}
	if _, ok := floatAttr(se, "brightness_pct"); ok {
		return true
	}
	if modes, ok := se.Attributes["supported_color_modes"].([]interface{}); ok {
		for _, m := range modes {
			if s, _ := m.(string); s == "brightness" || s == "color_temp" || s == "hs" || s == "xy" || s == "rgb" {
				return true
			}
		}
	}
	if sf, ok := floatAttr(se, "supported_features"); ok {
		// light.SUPPORT_BRIGHTNESS = 1
		if int(sf)&1 != 0 {
			return true
		}
	}
	return false
}
