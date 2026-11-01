package mapper

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	haclient "github.com/mkelcik/go-ha-client/v2"
)

// Device binds one HA entity to a bridged HAP accessory.
type Device struct {
	EntityID string
	Domain   string
	A        *accessory.A
	applyHA  func(haclient.StateEntity)
}

// ApplyHA pushes a Home Assistant state update into HomeKit.
func (d *Device) ApplyHA(se haclient.StateEntity) {
	if d.applyHA != nil {
		d.applyHA(se)
	}
}

type HAActions interface {
	CallEntity(ctx context.Context, domain, service, entityID string, data map[string]interface{}) error
}

func Build(entityID, friendlyName string, hapID int, se *haclient.StateEntity, ha HAActions, guard *SyncGuard) (*Device, error) {
	domain := Domain(entityID)
	if !MappableDomains[domain] {
		return nil, ErrUnsupported
	}
	info := accessory.Info{
		Name:         friendlyName,
		SerialNumber: entityID,
		Manufacturer: "Home Assistant",
		Model:        domain,
		Firmware:     "1.0",
	}

	var dev *Device
	switch domain {
	case "switch", "input_boolean":
		if se != nil && domain == "switch" && supportsLightLevel(*se) {
			dev = buildDimmer(info, entityID, "switch", ha, guard)
		} else {
			dev = buildSwitch(info, entityID, domain, ha, guard)
		}
	case "light":
		dev = buildDimmer(info, entityID, "light", ha, guard)
	case "cover":
		if se != nil && isGarageCover(*se) {
			dev = buildGarageDoor(info, entityID, ha, guard)
		} else {
			dev = buildCover(info, entityID, ha, guard)
		}
	case "fan":
		dev = buildFan(info, entityID, ha, guard)
	case "lock":
		dev = buildLock(info, entityID, ha, guard)
	case "climate":
		dev = buildClimate(info, entityID, ha, guard)
	case "humidifier":
		dev = buildHumidifier(info, entityID, ha, guard)
	case "water_heater":
		dev = buildWaterHeater(info, entityID, ha, guard)
	case "valve":
		dev = buildValve(info, entityID, ha, guard)
	case "dehumidifier":
		dev = buildDehumidifier(info, entityID, ha, guard)
	case "binary_sensor":
		var err error
		dev, err = buildBinarySensor(info, entityID, se, ha, guard)
		if err != nil {
			return nil, err
		}
	case "siren":
		dev = buildSiren(info, entityID, ha, guard)
	case "alarm_control_panel":
		dev = buildAlarmPanel(info, entityID, ha, guard)
	case "button", "input_button":
		dev = buildButton(info, entityID, domain, ha, guard)
	default:
		return nil, ErrUnsupported
	}
	dev.A.Id = uint64(hapID)
	if se != nil {
		dev.applyHA(*se)
	}
	return dev, nil
}

var ErrUnsupported = errUnsupported("domain not supported")

type errUnsupported string

func (e errUnsupported) Error() string { return string(e) }

func buildSwitch(info accessory.Info, entityID, domain string, ha HAActions, guard *SyncGuard) *Device {
	sw := accessory.NewSwitch(info)
	d := &Device{EntityID: entityID, Domain: domain, A: sw.A}
	d.applyHA = func(se haclient.StateEntity) {
		on := se.State == "on"
		guard.WhileHA(func() {
			sw.Switch.On.SetValue(on)
		})
	}
	sw.Switch.On.OnValueRemoteUpdate(func(on bool) {
		if guard.FromHA() {
			return
		}
		svc := "turn_off"
		if on {
			svc = "turn_on"
		}
		go callHA(ha, domain, svc, entityID, nil)
	})
	return d
}

// buildDimmer exposes on/off and brightness (HomeKit light with level slider).
func buildDimmer(info accessory.Info, entityID, serviceDomain string, ha HAActions, guard *SyncGuard) *Device {
	lt := accessory.NewLightbulb(info)
	bright := characteristic.NewBrightness()
	lt.Lightbulb.AddC(bright.C)

	d := &Device{EntityID: entityID, Domain: serviceDomain, A: lt.A}
	lastLevel := 100

	d.applyHA = func(se haclient.StateEntity) {
		on := se.State == "on"
		level, _ := lightLevelHAP(se)
		if on {
			lastLevel = level
		}
		guard.WhileHA(func() {
			lt.Lightbulb.On.SetValue(on)
			if on {
				bright.SetValue(level)
			} else {
				bright.SetValue(0)
			}
		})
	}

	lt.Lightbulb.On.OnValueRemoteUpdate(func(on bool) {
		if guard.FromHA() {
			return
		}
		if !on {
			go callHA(ha, serviceDomain, "turn_off", entityID, nil)
			return
		}
		data := dimmerServiceData(serviceDomain, lastLevel)
		go callHA(ha, serviceDomain, "turn_on", entityID, data)
	})

	bright.OnValueRemoteUpdate(func(v int) {
		if guard.FromHA() {
			return
		}
		lastLevel = clampInt(v, 1, 100)
		data := dimmerServiceData(serviceDomain, lastLevel)
		go callHA(ha, serviceDomain, "turn_on", entityID, data)
	})

	return d
}

func dimmerServiceData(serviceDomain string, level int) map[string]interface{} {
	level = clampInt(level, 1, 100)
	if serviceDomain == "light" {
		return map[string]interface{}{
			"brightness": brightnessHA(float64(level)),
		}
	}
	// Some dimmer switches accept brightness or brightness_pct on turn_on.
	return map[string]interface{}{
		"brightness_pct": level,
		"brightness":     brightnessHA(float64(level)),
	}
}

func buildCover(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	cv := accessory.NewWindowCovering(info)
	d := &Device{EntityID: entityID, Domain: "cover", A: cv.A}
	d.applyHA = func(se haclient.StateEntity) {
		pos := coverPosition(se)
		guard.WhileHA(func() {
			p := int(pos)
			cv.WindowCovering.CurrentPosition.SetValue(p)
			cv.WindowCovering.TargetPosition.SetValue(p)
		})
	}
	cv.WindowCovering.TargetPosition.OnValueRemoteUpdate(func(target int) {
		if guard.FromHA() {
			return
		}
		if target <= 5 {
			go callHA(ha, "cover", "close_cover", entityID, nil)
			return
		}
		if target >= 95 {
			go callHA(ha, "cover", "open_cover", entityID, nil)
			return
		}
		go callHA(ha, "cover", "set_cover_position", entityID, map[string]interface{}{
			"position": target,
		})
	})
	return d
}

func buildFan(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	fn := accessory.NewFan(info)
	rot := characteristic.NewRotationSpeed()
	fn.Fan.AddC(rot.C)

	d := &Device{EntityID: entityID, Domain: "fan", A: fn.A}
	lastSpeed := 100

	d.applyHA = func(se haclient.StateEntity) {
		on := se.State == "on"
		speed := fanSpeedHAP(se)
		if on {
			lastSpeed = speed
		}
		guard.WhileHA(func() {
			fn.Fan.On.SetValue(on)
			if on {
				rot.SetValue(float64(speed))
			} else {
				rot.SetValue(0)
			}
		})
	}

	fn.Fan.On.OnValueRemoteUpdate(func(on bool) {
		if guard.FromHA() {
			return
		}
		if !on {
			go callHA(ha, "fan", "turn_off", entityID, nil)
			return
		}
		go callHA(ha, "fan", "turn_on", entityID, map[string]interface{}{
			"percentage": fanPercentageHA(lastSpeed),
		})
	})

	rot.OnValueRemoteUpdate(func(speed float64) {
		if guard.FromHA() {
			return
		}
		pct := fanPercentageHA(int(speed))
		lastSpeed = pct
		if pct == 0 {
			go callHA(ha, "fan", "turn_off", entityID, nil)
			return
		}
		go callHA(ha, "fan", "turn_on", entityID, map[string]interface{}{
			"percentage": pct,
		})
	})

	return d
}

func buildLock(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	dr := accessory.NewDoor(info)
	d := &Device{EntityID: entityID, Domain: "lock", A: dr.A}
	d.applyHA = func(se haclient.StateEntity) {
		pos := 0.0
		if se.State == "unlocked" {
			pos = 100
		}
		guard.WhileHA(func() {
			p := int(pos)
			dr.Door.CurrentPosition.SetValue(p)
			dr.Door.TargetPosition.SetValue(p)
		})
	}
	dr.Door.TargetPosition.OnValueRemoteUpdate(func(target int) {
		if guard.FromHA() {
			return
		}
		if target >= 50 {
			go callHA(ha, "lock", "unlock", entityID, nil)
		} else {
			go callHA(ha, "lock", "lock", entityID, nil)
		}
	})
	return d
}

func buildClimate(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	th := accessory.NewThermostat(info)
	d := &Device{EntityID: entityID, Domain: "climate", A: th.A}
	var lastUnit tempUnit = tempUnitCelsius
	d.applyHA = func(se haclient.StateEntity) {
		unit := climateUnitFromState(se)
		lastUnit = unit
		guard.WhileHA(func() {
			th.Thermostat.CurrentHeatingCoolingState.SetValue(int(climateCurrentState(se.State)))
			th.Thermostat.TargetHeatingCoolingState.SetValue(int(climateTargetState(se.State)))
			th.Thermostat.TemperatureDisplayUnits.SetValue(hapDisplayUnits(unit))
			if t, ok := floatAttr(se, "current_temperature"); ok {
				th.Thermostat.CurrentTemperature.SetValue(haTempToHAP(t, unit))
			}
			if t, ok := floatAttr(se, "temperature"); ok {
				th.Thermostat.TargetTemperature.SetValue(haTempToHAP(t, unit))
			}
		})
	}
	th.Thermostat.TargetTemperature.OnValueRemoteUpdate(func(t float64) {
		if guard.FromHA() {
			return
		}
		// HAP reports target temp in Celsius.
		target := hapTempToHA(t, lastUnit)
		go callHA(ha, "climate", "set_temperature", entityID, map[string]interface{}{
			"temperature": target,
		})
	})
	th.Thermostat.TargetHeatingCoolingState.OnValueRemoteUpdate(func(mode int) {
		if guard.FromHA() {
			return
		}
		hvac := "off"
		switch mode {
		case 1:
			hvac = "heat"
		case 2:
			hvac = "cool"
		}
		go callHA(ha, "climate", "set_hvac_mode", entityID, map[string]interface{}{
			"hvac_mode": hvac,
		})
	})
	return d
}

func buildHumidifier(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	hm := accessory.NewHumidifier(info)
	d := &Device{EntityID: entityID, Domain: "humidifier", A: hm.A}
	d.applyHA = func(se haclient.StateEntity) {
		on := se.State == "on"
		active := 0
		if on {
			active = 1
		}
		guard.WhileHA(func() {
			hm.Humidifier.Active.SetValue(active)
		})
	}
	hm.Humidifier.Active.OnValueRemoteUpdate(func(active int) {
		if guard.FromHA() {
			return
		}
		svc := "turn_off"
		if active != 0 {
			svc = "turn_on"
		}
		go callHA(ha, "humidifier", svc, entityID, nil)
	})
	return d
}

func buildWaterHeater(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	ht := accessory.NewHeater(info)
	d := &Device{EntityID: entityID, Domain: "water_heater", A: ht.A}
	d.applyHA = func(se haclient.StateEntity) {
		on := se.State != "off"
		active := 0
		if on {
			active = 1
		}
		guard.WhileHA(func() {
			ht.Heater.Active.SetValue(active)
		})
	}
	ht.Heater.Active.OnValueRemoteUpdate(func(active int) {
		if guard.FromHA() {
			return
		}
		if active != 0 {
			go callHA(ha, "water_heater", "turn_on", entityID, nil)
		} else {
			go callHA(ha, "water_heater", "turn_off", entityID, nil)
		}
	})
	return d
}

func callHA(ha HAActions, domain, service, entityID string, data map[string]interface{}) {
	if err := ha.CallEntity(context.Background(), domain, service, entityID, data); err != nil {
		log.Printf("ha call %s.%s for %s: %v", domain, service, entityID, err)
	}
}

func floatAttr(se haclient.StateEntity, key string) (float64, bool) {
	if se.Attributes == nil {
		return 0, false
	}
	v, ok := se.Attributes[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case jsonNumber:
		f, err := t.Float64()
		return f, err == nil
	default:
		s := strings.TrimSpace(toString(v))
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func brightnessHAP(ha float64) float64 {
	if ha <= 0 {
		return 0
	}
	return (ha / 255.0) * 100.0
}

func brightnessHA(hap float64) int {
	if hap <= 0 {
		return 0
	}
	return int(hap / 100.0 * 255.0)
}

func coverPosition(se haclient.StateEntity) float64 {
	if p, ok := floatAttr(se, "current_position"); ok {
		return p
	}
	switch se.State {
	case "open":
		return 100
	case "closed":
		return 0
	default:
		return 50
	}
}

func climateCurrentState(state string) float64 {
	switch state {
	case "heat":
		return 1
	case "cool":
		return 2
	default:
		return 0
	}
}

func climateTargetState(state string) float64 {
	return climateCurrentState(state)
}
