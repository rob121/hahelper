package camera

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/brutella/hap/rtp"
)

// StreamID identifies a HomeKit streaming session.
type StreamID string

// Input describes an ffmpeg input (HA MJPEG proxy or direct RTSP).
type Input struct {
	// RTSP set when streaming directly (skips HTTP headers).
	RTSP string
	// HTTPURL is the ffmpeg input URL.
	HTTPURL string
	Token   string
	// ViaLocalProxy means HTTPURL is hahelper's /api/camera/* (no Bearer to HA).
	ViaLocalProxy bool
	// SnapshotLoop uses periodic still frames when true (HTTPURL should be camera_proxy).
	SnapshotLoop bool
}

// FFmpegBackend transcodes an input into SRTP for HomeKit.
type FFmpegBackend struct {
	Input          Input
	H264Encoder    string
	MinVideoBitrate int

	mu      sync.Mutex
	streams map[StreamID]*ffmpegSession
}

func NewFFmpegBackend(in Input) *FFmpegBackend {
	enc := "libx264"
	if in.RTSP != "" {
		enc = "copy"
	}
	return &FFmpegBackend{
		Input:           in,
		H264Encoder:     enc,
		MinVideoBitrate: 300,
		streams:         make(map[StreamID]*ffmpegSession),
	}
}

func (f *FFmpegBackend) PrepareNewStream(req rtp.SetupEndpoints, resp rtp.SetupEndpointsResponse) StreamID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := StreamID(string(req.SessionId))
	f.streams[id] = &ffmpegSession{req: req, resp: resp, parent: f}
	return id
}

func (f *FFmpegBackend) Start(id StreamID, video rtp.VideoParameters, audio rtp.AudioParameters) error {
	f.mu.Lock()
	s, ok := f.streams[id]
	if !ok && len(f.streams) == 1 {
		for _, only := range f.streams {
			s = only
			ok = true
			break
		}
	}
	f.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown stream %s", id)
	}
	return s.start(video)
}

func (f *FFmpegBackend) Stop(id StreamID) {
	f.mu.Lock()
	s, ok := f.streams[id]
	if ok {
		s.stop()
		delete(f.streams, id)
	}
	f.mu.Unlock()
}

func (f *FFmpegBackend) Suspend(id StreamID) {
	f.mu.Lock()
	s, ok := f.streams[id]
	f.mu.Unlock()
	if ok && s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGSTOP)
	}
}

func (f *FFmpegBackend) Resume(id StreamID) {
	f.mu.Lock()
	s, ok := f.streams[id]
	f.mu.Unlock()
	if ok && s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGCONT)
	}
}

func (f *FFmpegBackend) Reconfigure(id StreamID, video rtp.VideoParameters, audio rtp.AudioParameters) error {
	return nil
}

// StopAll ends every active ffmpeg session (call on bridge reload).
func (f *FFmpegBackend) StopAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.streams {
		s.stop()
		delete(f.streams, id)
	}
}

type ffmpegSession struct {
	parent *FFmpegBackend
	req    rtp.SetupEndpoints
	resp   rtp.SetupEndpointsResponse
	cmd    *exec.Cmd
}

func (s *ffmpegSession) start(video rtp.VideoParameters) error {
	in := s.parent.Input
	args := []string{"-hide_banner", "-loglevel", "warning"}
	if in.RTSP != "" {
		args = append(args, "-rtsp_transport", "tcp", "-i", in.RTSP)
	} else {
		if !in.ViaLocalProxy && in.Token != "" {
			args = append(args, "-headers", fmt.Sprintf("Authorization: Bearer %s\r\n", in.Token))
		}
		if in.SnapshotLoop {
			args = append(args, "-re", "-stream_loop", "-1", "-f", "image2", "-framerate", "1", "-i", in.HTTPURL)
		} else {
			// Let ffmpeg probe multipart MJPEG from HA (do not force -f mjpeg before -i).
			args = append(args,
				"-reconnect", "1", "-reconnect_at_eof", "1",
				"-reconnect_streamed", "1", "-reconnect_delay_max", "2",
				"-i", in.HTTPURL,
			)
		}
	}

	width := video.Attributes.Width
	if width == 0 {
		width = 1280
	}
	fps := video.Attributes.Framerate
	if fps == 0 {
		fps = 15
	}
	br := int(video.RTP.Bitrate)
	if br < s.parent.MinVideoBitrate {
		br = s.parent.MinVideoBitrate
	}

	encoder := s.parent.H264Encoder
	if in.SnapshotLoop {
		encoder = "libx264"
	}

	mtu := videoMTU(s.req)
	args = append(args, "-an", "-codec:v", encoder, "-pix_fmt", "yuv420p", "-vsync", "vfr")
	// MJPEG is full-range (yuvj); scale + limited range for H.264 RTP.
	if encoder == "libx264" && !in.SnapshotLoop && in.RTSP == "" {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:-2:in_range=jpeg:out_range=mpeg,format=yuv420p", width))
	} else {
		args = append(args, "-video_size", fmt.Sprintf("%d:-2", width))
	}
	args = append(args, "-framerate", fmt.Sprintf("%d", fps))
	if encoder == "libx264" {
		if lvl := h264Level(video.CodecParams); lvl != "" {
			args = append(args, "-level:v", lvl)
		}
	}
	args = append(args,
		"-b:v", fmt.Sprintf("%dk", br),
		"-payload_type", fmt.Sprintf("%d", video.RTP.PayloadType),
		"-ssrc", fmt.Sprintf("%d", s.resp.SsrcVideo),
		"-f", "rtp",
		"-srtp_out_suite", "AES_CM_128_HMAC_SHA1_80",
		"-srtp_out_params", s.req.Video.SrtpKey(),
		fmt.Sprintf("srtp://%s:%d?rtcpport=%d&pkt_size=%s&timeout=60",
			s.req.ControllerAddr.IPAddr,
			s.req.ControllerAddr.VideoRtpPort,
			s.req.ControllerAddr.VideoRtpPort,
			mtu,
		),
	)

	s.stop()
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	log.Printf("camera ffmpeg: %s", strings.Join(redactFFmpegArgs(args), " "))
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd = cmd
	return nil
}

func (s *ffmpegSession) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGINT)
		_ = s.cmd.Wait()
	}
	s.cmd = nil
}

func redactFFmpegArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-headers":
			out = append(out, "-headers", "Authorization: Bearer ***")
			if i+1 < len(args) {
				i++
			}
		case "-srtp_out_params":
			out = append(out, "-srtp_out_params", "***")
			if i+1 < len(args) {
				i++
			}
		default:
			out = append(out, args[i])
		}
	}
	return out
}

func h264Level(params rtp.VideoCodecParameters) string {
	for _, l := range params.Levels {
		switch l.Level {
		case rtp.VideoCodecLevel3_1:
			return "3.1"
		case rtp.VideoCodecLevel3_2:
			return "3.2"
		case rtp.VideoCodecLevel4:
			return "4.0"
		}
	}
	return "3.1"
}

func videoMTU(setup rtp.SetupEndpoints) string {
	switch setup.ControllerAddr.IPVersion {
	case rtp.IPAddrVersionv6:
		return "1228"
	default:
		return "1378"
	}
}