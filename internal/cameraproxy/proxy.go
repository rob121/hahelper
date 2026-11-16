package cameraproxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	ha "github.com/mkelcik/go-ha-client/v2"

	"github.com/rob121/hahelper/internal/config"
	"github.com/rob121/hahelper/internal/exposure"
	"github.com/rob121/hahelper/internal/haclient"
)

var streamClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	},
}

// LocalListenHost returns a loopback host:port suitable for local ffmpeg (from ui.listen).
func LocalListenHost(listen string) (string, error) {
	if listen == "" {
		return "", fmt.Errorf("empty ui.listen")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// StreamURL is the URL ffmpeg should read (no HA token required).
func StreamURL(listen, entityID string) (string, error) {
	addr, err := LocalListenHost(listen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s/api/camera/stream/%s", addr, url.PathEscape(entityID)), nil
}

// SnapshotURL is for still-image loop mode.
func SnapshotURL(listen, entityID string) (string, error) {
	addr, err := LocalListenHost(listen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s/api/camera/snapshot/%s", addr, url.PathEscape(entityID)), nil
}

// Handler proxies camera feeds from Home Assistant for local consumers (ffmpeg).
type Handler struct {
	cfg      *config.Config
	exposure *exposure.Registry
}

func NewHandler(cfg *config.Config, exp *exposure.Registry) *Handler {
	return &Handler{cfg: cfg, exposure: exp}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/api/camera/stream/", h.handleStream)
	mux.HandleFunc("/api/camera/snapshot/", h.handleSnapshot)
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entityID := strings.TrimPrefix(r.URL.Path, "/api/camera/stream/")
	entityID, _ = url.PathUnescape(entityID)
	if !h.allow(entityID) {
		http.Error(w, "not allowed", http.StatusForbidden)
		return
	}
	haURL, token, err := h.haCreds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	u := haclient.CameraProxyStreamURL(haURL, entityID)
	h.forwardStream(r.Context(), w, u, token)
}

func (h *Handler) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entityID := strings.TrimPrefix(r.URL.Path, "/api/camera/snapshot/")
	entityID, _ = url.PathUnescape(entityID)
	if !h.allow(entityID) {
		http.Error(w, "not allowed", http.StatusForbidden)
		return
	}
	haURL, token, err := h.haCreds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	u := haclient.CameraProxyURL(haURL, entityID)
	h.forward(r.Context(), w, u, token)
}

func (h *Handler) allow(entityID string) bool {
	domain, _, _ := ha.ParseEntityID(entityID)
	if entityID == "" || domain != "camera" {
		return false
	}
	exposed := h.exposure.ExposedSet()
	_, ok := exposed[entityID]
	return ok
}

func (h *Handler) haCreds() (string, string, error) {
	if h.cfg == nil {
		return "", "", fmt.Errorf("ha not configured")
	}
	c := *h.cfg
	if !c.HAConfigured() {
		return "", "", fmt.Errorf("ha not configured")
	}
	return c.HomeAssistant.URL, strings.TrimSpace(c.HomeAssistant.Token), nil
}

func (h *Handler) forward(ctx context.Context, w http.ResponseWriter, upstream, token string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	resp, err := h.upstreamGET(ctx, upstream, token)
	if err != nil {
		log.Printf("camera proxy upstream %s: %v", upstream, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("camera proxy %s: status %d %s", upstream, resp.StatusCode, strings.TrimSpace(string(body)))
		http.Error(w, fmt.Sprintf("home assistant returned %d", resp.StatusCode), resp.StatusCode)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil && ctx.Err() == nil {
		log.Printf("camera proxy copy: %v", err)
	}
}

// forwardStream keeps one client response open and reconnects to HA when the upstream MJPEG ends.
func (h *Handler) forwardStream(ctx context.Context, w http.ResponseWriter, upstream, token string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var started bool
	for ctx.Err() == nil {
		resp, err := h.upstreamGET(ctx, upstream, token)
		if err != nil {
			if !started {
				log.Printf("camera proxy upstream %s: %v", upstream, err)
				http.Error(w, err.Error(), http.StatusBadGateway)
			} else {
				log.Printf("camera proxy reconnect %s: %v", upstream, err)
			}
			return
		}
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			log.Printf("camera proxy %s: status %d %s", upstream, resp.StatusCode, strings.TrimSpace(string(body)))
			if !started {
				http.Error(w, fmt.Sprintf("home assistant returned %d", resp.StatusCode), resp.StatusCode)
			}
			return
		}
		if !started {
			if ct := resp.Header.Get("Content-Type"); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			w.WriteHeader(http.StatusOK)
			started = true
		}
		_, err = io.Copy(w, resp.Body)
		resp.Body.Close()
		if err != nil && ctx.Err() == nil {
			log.Printf("camera proxy stream ended %s: %v (reconnecting)", upstream, err)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (h *Handler) upstreamGET(ctx context.Context, upstream, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "*/*")
	return streamClient.Do(req)
}

// ExposedCamera returns true if entity is an exposed camera (for tests).
func (h *Handler) ExposedCamera(entityID string) bool {
	return h.allow(entityID)
}