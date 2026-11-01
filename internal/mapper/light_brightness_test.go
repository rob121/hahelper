package mapper

import (
	"testing"

	haclient "github.com/mkelcik/go-ha-client/v2"
)

func TestLightLevelHAP_brightness255(t *testing.T) {
	se := haclient.StateEntity{
		State: "on",
		Attributes: map[string]interface{}{
			"brightness": float64(255),
		},
	}
	got, ok := lightLevelHAP(se)
	if !ok || got != 100 {
		t.Fatalf("got %d ok=%v", got, ok)
	}
}

func TestLightLevelHAP_brightnessPct(t *testing.T) {
	se := haclient.StateEntity{
		State: "on",
		Attributes: map[string]interface{}{
			"brightness_pct": float64(40),
		},
	}
	got, ok := lightLevelHAP(se)
	if !ok || got != 40 {
		t.Fatalf("got %d", got)
	}
}

func TestFanSpeedHAP_percentage(t *testing.T) {
	se := haclient.StateEntity{
		State: "on",
		Attributes: map[string]interface{}{
			"percentage": float64(50),
		},
	}
	if fanSpeedHAP(se) != 50 {
		t.Fatal("expected 50")
	}
}
