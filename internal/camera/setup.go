// RTP stream setup adapted from github.com/brutella/hkcam (MIT).
package camera

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"reflect"
	"strings"

	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	"github.com/brutella/hap/rtp"
	"github.com/brutella/hap/service"
	"github.com/brutella/hap/tlv8"
)

// StreamBackend handles HomeKit RTP session lifecycle (see ffmpeg.go).
type StreamBackend interface {
	PrepareNewStream(req rtp.SetupEndpoints, resp rtp.SetupEndpointsResponse) StreamID
	Start(id StreamID, video rtp.VideoParameters, audio rtp.AudioParameters) error
	Stop(id StreamID)
	Suspend(id StreamID)
	Resume(id StreamID)
	Reconfigure(id StreamID, video rtp.VideoParameters, audio rtp.AudioParameters) error
}

// ConfigureAccessory wires HomeKit camera RTP services to the stream backend.
func ConfigureAccessory(cam *accessory.Camera, backend StreamBackend, multiStream bool) {
	if cam.StreamManagement2 != nil && cam.StreamManagement2.S != nil {
		cam.AddS(cam.StreamManagement2.S)
	}
	setupStreamManagement(cam.StreamManagement1, backend, multiStream)
	if cam.StreamManagement2 != nil {
		setupStreamManagement(cam.StreamManagement2, backend, multiStream)
	}
}

func setupStreamManagement(m *service.CameraRTPStreamManagement, backend StreamBackend, multiStream bool) {
	setTLV8(m.StreamingStatus.Bytes, rtp.StreamingStatus{Status: rtp.StreamingStatusAvailable})
	setTLV8(m.SupportedRTPConfiguration.Bytes, rtp.NewConfiguration(rtp.CryptoSuite_AES_CM_128_HMAC_SHA1_80))
	setTLV8(m.SupportedVideoStreamConfiguration.Bytes, rtp.DefaultVideoStreamConfiguration())
	setTLV8(m.SupportedAudioStreamConfiguration.Bytes, rtp.DefaultAudioStreamConfiguration())

	m.SelectedRTPStreamConfiguration.OnValueRemoteUpdate(func(buf []byte) {
		var cfg rtp.StreamConfiguration
		if err := tlv8.Unmarshal(buf, &cfg); err != nil {
			log.Printf("camera SelectedRTPStreamConfiguration: %v", err)
			return
		}
		id := StreamID(string(cfg.Command.Identifier))
		switch cfg.Command.Type {
		case rtp.SessionControlCommandTypeStart:
			if err := backend.Start(id, cfg.Video, cfg.Audio); err != nil {
				log.Printf("camera stream start %s: %v", id, err)
			}
			if !multiStream {
				setTLV8(m.StreamingStatus.Bytes, rtp.StreamingStatus{Status: rtp.StreamingStatusBusy})
			}
		case rtp.SessionControlCommandTypeSuspend:
			backend.Suspend(id)
		case rtp.SessionControlCommandTypeResume:
			backend.Resume(id)
		case rtp.SessionControlCommandTypeReconfigure:
			if err := backend.Reconfigure(id, cfg.Video, cfg.Audio); err != nil {
				log.Printf("camera reconfigure: %v", err)
			}
		case rtp.SessionControlCommandTypeEnd:
			backend.Stop(id)
			setTLV8(m.StreamingStatus.Bytes, rtp.StreamingStatus{Status: rtp.StreamingStatusAvailable})
		default:
			log.Printf("camera: unknown session command %d", cfg.Command.Type)
		}
	})

	m.SetupEndpoints.OnValueUpdate(func(new, old []byte, r *http.Request) {
		if r == nil {
			return
		}
		var req rtp.SetupEndpoints
		if err := tlv8.Unmarshal(new, &req); err != nil {
			log.Printf("camera SetupEndpoints: %v", err)
			return
		}
		iface, err := ifaceOfRequest(r)
		if err != nil {
			log.Printf("camera interface: %v", err)
			return
		}
		ip, err := ipAtInterface(*iface, req.ControllerAddr.IPVersion)
		if err != nil {
			log.Printf("camera local ip: %v", err)
			return
		}
		ssrcVideo := rand.Int31()
		ssrcAudio := rand.Int31()
		resp := rtp.SetupEndpointsResponse{
			SessionId: req.SessionId,
			Status:    rtp.SessionStatusSuccess,
			AccessoryAddr: rtp.Addr{
				IPVersion:    req.ControllerAddr.IPVersion,
				IPAddr:       ip.String(),
				VideoRtpPort: req.ControllerAddr.VideoRtpPort,
				AudioRtpPort: req.ControllerAddr.AudioRtpPort,
			},
			Video:     req.Video,
			Audio:     req.Audio,
			SsrcVideo: ssrcVideo,
			SsrcAudio: ssrcAudio,
		}
		backend.PrepareNewStream(req, resp)
		log.Printf("camera setup: controller=%s:%d accessory=%s:%d/%d",
			req.ControllerAddr.IPAddr, req.ControllerAddr.VideoRtpPort,
			ip.String(), req.ControllerAddr.VideoRtpPort, req.ControllerAddr.AudioRtpPort)
		setTLV8(m.SetupEndpoints.Bytes, resp)
	})
}

func setTLV8(c *characteristic.Bytes, v interface{}) {
	b, err := tlv8.Marshal(v)
	if err != nil {
		log.Printf("camera tlv8: %v", err)
		return
	}
	c.SetValue(b)
}

func ipAtInterface(iface net.Interface, version uint8) (net.IP, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		switch version {
		case rtp.IPAddrVersionv4:
			if ip.To4() != nil {
				return ip, nil
			}
		case rtp.IPAddrVersionv6:
			if ip.To16() != nil && ip.To4() == nil {
				return ip, nil
			}
		}
	}
	return nil, fmt.Errorf("no address for interface %s version %d", iface.Name, version)
}

func ifaceOfRequest(r *http.Request) (*net.Interface, error) {
	v := r.Context().Value(http.LocalAddrContextKey)
	if v == nil {
		return nil, fmt.Errorf("no local address in context")
	}
	host, _, err := net.SplitHostPort(v.(net.Addr).String())
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		comps := strings.Split(host, "%")
		if len(comps) == 2 {
			return net.InterfaceByName(comps[1])
		}
		return nil, fmt.Errorf("unable to parse local ip %q", host)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			addrIP, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				return nil, err
			}
			if reflect.DeepEqual(addrIP, ip) {
				return &iface, nil
			}
		}
	}
	return nil, fmt.Errorf("could not find interface for connection")
}