package mapper

import (
	"testing"

	haclient "github.com/mkelcik/go-ha-client/v2"
)

func TestHaTempToHAP_fahrenheit(t *testing.T) {
	got := haTempToHAP(73, tempUnitFahrenheit)
	want := 22.777777777777776
	if diff := got - want; diff > 0.01 || diff < -0.01 {
		t.Fatalf("73°F → %v°C, want ~%v", got, want)
	}
}

func TestClimateUnitFromState_explicitF(t *testing.T) {
	se := haclient.StateEntity{
		Attributes: map[string]interface{}{
			"temperature_unit":     "°F",
			"current_temperature":  73,
			"temperature":          72,
		},
	}
	if climateUnitFromState(se) != tempUnitFahrenheit {
		t.Fatal("expected fahrenheit")
	}
}

func TestClimateUnitFromState_heuristic(t *testing.T) {
	se := haclient.StateEntity{
		Attributes: map[string]interface{}{
			"current_temperature": 73,
			"temperature":         72,
		},
	}
	if climateUnitFromState(se) != tempUnitFahrenheit {
		t.Fatal("expected fahrenheit heuristic for 73/72")
	}
}
