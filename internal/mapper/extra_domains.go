package mapper

import (
	"strings"

	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	"github.com/brutella/hap/service"
	haclient "github.com/mkelcik/go-ha-client/v2"
)

func buildValve(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	a := accessory.New(info, accessory.TypeSprinkler)
	valve := service.NewValve()
	valve.ValveType.SetValue(characteristic.ValveTypeIrrigation)
	a.AddS(valve.S)

	d := &Device{EntityID: entityID, Domain: "valve", A: a}
	d.applyHA = func(se haclient.StateEntity) {
		active, inUse := valveActiveFromHA(se.State)
		guard.WhileHA(func() {
			valve.Active.SetValue(active)
			valve.InUse.SetValue(inUse)
		})
	}
	valve.Active.OnValueRemoteUpdate(func(active int) {
		if guard.FromHA() {
			return
		}
		if active != characteristic.ActiveInactive {
			go callHA(ha, "valve", "open", entityID, nil)
		} else {
			go callHA(ha, "valve", "close", entityID, nil)
		}
	})
	return d
}

func valveActiveFromHA(state string) (active, inUse int) {
	switch strings.ToLower(state) {
	case "open", "on":
		return characteristic.ActiveActive, 0
	case "opening", "closing":
		return characteristic.ActiveActive, 1
	default:
		return characteristic.ActiveInactive, 0
	}
}

func buildDehumidifier(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	dh := accessory.NewDehumidifier(info)
	d := &Device{EntityID: entityID, Domain: "dehumidifier", A: dh.A}
	d.applyHA = func(se haclient.StateEntity) {
		active := 0
		if se.State == "on" {
			active = 1
		}
		guard.WhileHA(func() {
			dh.Dehumidifier.Active.SetValue(active)
		})
	}
	dh.Dehumidifier.Active.OnValueRemoteUpdate(func(active int) {
		if guard.FromHA() {
			return
		}
		svc := "turn_off"
		if active != 0 {
			svc = "turn_on"
		}
		go callHA(ha, "dehumidifier", svc, entityID, nil)
	})
	return d
}

func buildBinarySensor(info accessory.Info, entityID string, se *haclient.StateEntity, ha HAActions, guard *SyncGuard) (*Device, error) {
	if se == nil {
		return nil, ErrUnsupported
	}
	switch binarySensorKind(*se) {
	case binaryKindContact:
		return buildContactSensor(info, entityID, ha, guard), nil
	case binaryKindMotion:
		return buildMotionSensor(info, entityID, ha, guard), nil
	default:
		return nil, ErrUnsupported
	}
}

func buildContactSensor(info accessory.Info, entityID string, _ HAActions, guard *SyncGuard) *Device {
	cs := accessory.NewContactSensor(info)
	d := &Device{EntityID: entityID, Domain: "binary_sensor", A: cs.A}
	d.applyHA = func(se haclient.StateEntity) {
		// HA door/window: on=open, off=closed → HAP contact detected when closed.
		detected := characteristic.ContactSensorStateContactNotDetected
		if se.State == "off" {
			detected = characteristic.ContactSensorStateContactDetected
		}
		guard.WhileHA(func() {
			cs.ContactSensor.ContactSensorState.SetValue(detected)
		})
	}
	return d
}

func buildMotionSensor(info accessory.Info, entityID string, _ HAActions, guard *SyncGuard) *Device {
	ms := accessory.NewMotionSensor(info)
	d := &Device{EntityID: entityID, Domain: "binary_sensor", A: ms.A}
	d.applyHA = func(se haclient.StateEntity) {
		guard.WhileHA(func() {
			ms.MotionSensor.MotionDetected.SetValue(se.State == "on")
		})
	}
	return d
}

func buildSiren(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	return buildSwitch(info, entityID, "siren", ha, guard)
}

func buildButton(info accessory.Info, entityID, domain string, ha HAActions, guard *SyncGuard) *Device {
	sw := accessory.NewSwitch(info)
	d := &Device{EntityID: entityID, Domain: domain, A: sw.A}
	d.applyHA = func(se haclient.StateEntity) {
		guard.WhileHA(func() {
			sw.Switch.On.SetValue(se.State == "on")
		})
	}
	sw.Switch.On.OnValueRemoteUpdate(func(on bool) {
		if guard.FromHA() || !on {
			return
		}
		go callHA(ha, domain, "press", entityID, nil)
	})
	return d
}

func buildAlarmPanel(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	sec := accessory.NewSecuritySystem(info)
	d := &Device{EntityID: entityID, Domain: "alarm_control_panel", A: sec.A}
	d.applyHA = func(se haclient.StateEntity) {
		cur, tgt := alarmStates(se.State)
		guard.WhileHA(func() {
			sec.SecuritySystem.SecuritySystemCurrentState.SetValue(cur)
			sec.SecuritySystem.SecuritySystemTargetState.SetValue(tgt)
		})
	}
	sec.SecuritySystem.SecuritySystemTargetState.OnValueRemoteUpdate(func(tgt int) {
		if guard.FromHA() {
			return
		}
		svc, code := alarmServiceForTarget(tgt)
		if svc == "" {
			return
		}
		data := map[string]interface{}{}
		if code != "" {
			data["code"] = code
		}
		go callHA(ha, "alarm_control_panel", svc, entityID, data)
	})
	return d
}

func alarmStates(state string) (current, target int) {
	switch strings.ToLower(state) {
	case "armed_home":
		return characteristic.SecuritySystemCurrentStateStayArm, characteristic.SecuritySystemTargetStateStayArm
	case "armed_away":
		return characteristic.SecuritySystemCurrentStateAwayArm, characteristic.SecuritySystemTargetStateAwayArm
	case "armed_night", "armed_vacation":
		return characteristic.SecuritySystemCurrentStateNightArm, characteristic.SecuritySystemTargetStateNightArm
	case "triggered", "pending":
		return characteristic.SecuritySystemCurrentStateAlarmTriggered, characteristic.SecuritySystemTargetStateDisarm
	default:
		return characteristic.SecuritySystemCurrentStateDisarmed, characteristic.SecuritySystemTargetStateDisarm
	}
}

func alarmServiceForTarget(tgt int) (service, code string) {
	switch tgt {
	case characteristic.SecuritySystemTargetStateStayArm:
		return "alarm_arm_home", ""
	case characteristic.SecuritySystemTargetStateAwayArm:
		return "alarm_arm_away", ""
	case characteristic.SecuritySystemTargetStateNightArm:
		return "alarm_arm_night", ""
	case characteristic.SecuritySystemTargetStateDisarm:
		return "alarm_disarm", ""
	default:
		return "", ""
	}
}

func buildGarageDoor(info accessory.Info, entityID string, ha HAActions, guard *SyncGuard) *Device {
	gd := accessory.NewGarageDoorOpener(info)
	d := &Device{EntityID: entityID, Domain: "cover", A: gd.A}
	d.applyHA = func(se haclient.StateEntity) {
		cur, tgt := garageDoorStates(se.State)
		guard.WhileHA(func() {
			gd.GarageDoorOpener.CurrentDoorState.SetValue(cur)
			gd.GarageDoorOpener.TargetDoorState.SetValue(tgt)
		})
	}
	gd.GarageDoorOpener.TargetDoorState.OnValueRemoteUpdate(func(tgt int) {
		if guard.FromHA() {
			return
		}
		if tgt == characteristic.TargetDoorStateOpen {
			go callHA(ha, "cover", "open_cover", entityID, nil)
		} else {
			go callHA(ha, "cover", "close_cover", entityID, nil)
		}
	})
	return d
}

func garageDoorStates(state string) (current, target int) {
	switch strings.ToLower(state) {
	case "open":
		return characteristic.CurrentDoorStateOpen, characteristic.CurrentDoorStateOpen
	case "opening":
		return characteristic.CurrentDoorStateOpening, characteristic.CurrentDoorStateOpen
	case "closing":
		return characteristic.CurrentDoorStateClosing, characteristic.CurrentDoorStateClosed
	default:
		return characteristic.CurrentDoorStateClosed, characteristic.CurrentDoorStateClosed
	}
}
