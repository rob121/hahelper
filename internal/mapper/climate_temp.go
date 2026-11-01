package mapper

import (
	"strings"

	"github.com/brutella/hap/characteristic"
	haclient "github.com/mkelcik/go-ha-client/v2"
)

type tempUnit int

const (
	tempUnitCelsius tempUnit = iota
	tempUnitFahrenheit
)

// climateUnitFromState detects whether HA reports thermostat temps in °F or °C.
func climateUnitFromState(se haclient.StateEntity) tempUnit {
	if se.Attributes == nil {
		return tempUnitCelsius
	}
	for _, key := range []string{"temperature_unit", "unit_of_measurement", "native_unit_of_measurement"} {
		if u, ok := se.Attributes[key]; ok {
			if isFahrenheitUnit(u) {
				return tempUnitFahrenheit
			}
			if isCelsiusUnit(u) {
				return tempUnitCelsius
			}
		}
	}
	// Heuristic: typical indoor °F readings are > 45 when target is sane; °C rarely above 45.
	if cur, ok := floatAttr(se, "current_temperature"); ok && cur > 45 {
		if tgt, ok2 := floatAttr(se, "temperature"); ok2 && tgt > 45 {
			return tempUnitFahrenheit
		}
		if ok {
			return tempUnitFahrenheit
		}
	}
	return tempUnitCelsius
}

func isFahrenheitUnit(v interface{}) bool {
	s := strings.ToUpper(strings.TrimSpace(toString(v)))
	return strings.Contains(s, "F") && !strings.Contains(s, "CF") // avoid false positives
}

func isCelsiusUnit(v interface{}) bool {
	s := strings.ToUpper(strings.TrimSpace(toString(v)))
	return strings.Contains(s, "C") && !strings.Contains(s, "F")
}

// haTempToHAP converts a Home Assistant temperature to Celsius for HAP characteristics.
func haTempToHAP(value float64, unit tempUnit) float64 {
	if unit == tempUnitFahrenheit {
		return (value - 32) * 5 / 9
	}
	return value
}

// hapTempToHA converts a HAP (Celsius) temperature back for Home Assistant.
func hapTempToHA(celsius float64, unit tempUnit) float64 {
	if unit == tempUnitFahrenheit {
		return celsius*9/5 + 32
	}
	return celsius
}

func hapDisplayUnits(unit tempUnit) int {
	if unit == tempUnitFahrenheit {
		return characteristic.TemperatureDisplayUnitsFahrenheit
	}
	return characteristic.TemperatureDisplayUnitsCelsius
}
