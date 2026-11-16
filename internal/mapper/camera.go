package mapper

import (
	"strings"

	"github.com/brutella/hap/accessory"
	haclient "github.com/mkelcik/go-ha-client/v2"
	"github.com/rob121/hahelper/internal/camera"
	"github.com/rob121/hahelper/internal/cameraproxy"
	apphaclient "github.com/rob121/hahelper/internal/haclient"
)

// CameraBridge configures HomeKit RTP streaming for a camera entity.
type CameraBridge interface {
	Attach(cam *accessory.Camera, entityID string) (stop func(), err error)
}

func buildCamera(info accessory.Info, entityID string, cams CameraBridge) (*Device, error) {
	if cams == nil {
		return nil, ErrUnsupported
	}
	cam := accessory.NewCamera(info)
	cam.Control.On.SetValue(true)
	stop, err := cams.Attach(cam, entityID)
	if err != nil {
		return nil, err
	}
	d := &Device{
		EntityID: entityID,
		Domain:   "camera",
		A:        cam.A,
		stop:     stop,
	}
	d.applyHA = func(haclient.StateEntity) {} // streaming is event-driven; no HA state mirror
	return d, nil
}

// HACameraBridge implements CameraBridge using Home Assistant stream URLs.
type HACameraBridge struct {
	HAURL           string
	Token           string
	UIListen        string
	MultiStream     bool
	StreamSources   map[string]string // entity_id -> rtsp:// override
	SnapshotCameras map[string]bool   // entities that only provide still images
	// ViaLocalProxy sends ffmpeg through hahelper's /api/camera/* (higher latency).
	ViaLocalProxy bool
}

func (h *HACameraBridge) Attach(cam *accessory.Camera, entityID string) (func(), error) {
	in := camera.Input{
		Token:         strings.TrimSpace(h.Token),
		ViaLocalProxy: h.ViaLocalProxy,
	}
	if rtsp := strings.TrimSpace(h.StreamSources[entityID]); rtsp != "" {
		in.RTSP = rtsp
		in.ViaLocalProxy = false
		in.Token = ""
	} else if h.ViaLocalProxy {
		var err error
		if h.SnapshotCameras[entityID] {
			in.HTTPURL, err = cameraproxy.SnapshotURL(h.UIListen, entityID)
			in.SnapshotLoop = true
		} else {
			in.HTTPURL, err = cameraproxy.StreamURL(h.UIListen, entityID)
		}
		if err != nil {
			return nil, err
		}
	} else if h.SnapshotCameras[entityID] {
		in.HTTPURL = apphaclient.CameraProxyURL(h.HAURL, entityID)
		in.SnapshotLoop = true
	} else {
		in.HTTPURL = apphaclient.CameraProxyStreamURL(h.HAURL, entityID)
	}
	if in.HTTPURL == "" && in.RTSP == "" {
		return nil, ErrUnsupported
	}
	backend := camera.NewFFmpegBackend(in)
	camera.ConfigureAccessory(cam, backend, h.MultiStream)
	return backend.StopAll, nil
}