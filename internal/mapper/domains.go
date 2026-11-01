package mapper

import (
	"sort"

	haclient "github.com/mkelcik/go-ha-client/v2"
)

// MappableDomains lists HA entity domains that may map to HomeKit (see IsExposable).
var MappableDomains = map[string]bool{
	"switch":              true,
	"input_boolean":       true,
	"light":               true,
	"cover":               true,
	"fan":                 true,
	"lock":                true,
	"climate":             true,
	"humidifier":          true,
	"dehumidifier":        true,
	"water_heater":        true,
	"valve":               true,
	"binary_sensor":       true,
	"siren":               true,
	"alarm_control_panel": true,
	"button":              true,
	"input_button":        true,
}

// ExposableDomains returns sorted domain names for the admin UI.
func ExposableDomains() []string {
	out := make([]string, 0, len(MappableDomains))
	for d := range MappableDomains {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func IsMappable(entityID string) bool {
	domain, _, err := haclient.ParseEntityID(entityID)
	if err != nil {
		return false
	}
	return MappableDomains[domain]
}

// IsExposable reports whether an entity can be bridged to HomeKit with current mappers.
func IsExposable(se haclient.StateEntity) bool {
	domain, _, err := haclient.ParseEntityID(se.EntityID)
	if err != nil || !MappableDomains[domain] {
		return false
	}
	if domain == "binary_sensor" {
		return binarySensorKind(se) != binaryKindUnsupported
	}
	return true
}

func Domain(entityID string) string {
	d, _, err := haclient.ParseEntityID(entityID)
	if err != nil {
		return ""
	}
	return d
}

type binaryKind int

const (
	binaryKindUnsupported binaryKind = iota
	binaryKindContact
	binaryKindMotion
)

func binarySensorKind(se haclient.StateEntity) binaryKind {
	dc := deviceClass(se)
	switch dc {
	case "door", "window", "opening", "garage_door", "dock", "gate":
		return binaryKindContact
	case "motion", "occupancy", "moving", "presence":
		return binaryKindMotion
	default:
		return binaryKindUnsupported
	}
}
