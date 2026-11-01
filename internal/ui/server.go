package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rob121/hahelper/internal/bridge"
	"github.com/rob121/hahelper/internal/config"
	"github.com/rob121/hahelper/internal/exposure"
	"github.com/rob121/hahelper/internal/haclient"
	"github.com/rob121/hahelper/internal/homekit"
	"github.com/rob121/hahelper/internal/mapper"
)

// Server is the admin UI and JSON API.
type Server struct {
	cfgPath  string
	cfg      *config.Config
	ha       **haclient.Service
	exposure *exposure.Registry
	bridge   **bridge.Manager
	onHAChange func()
	mu       sync.RWMutex
}

func New(
	cfgPath string,
	cfg *config.Config,
	ha **haclient.Service,
	exp *exposure.Registry,
	br **bridge.Manager,
	onHAChange func(),
) *Server {
	return &Server{
		cfgPath:    cfgPath,
		cfg:        cfg,
		ha:         ha,
		exposure:   exp,
		bridge:     br,
		onHAChange: onHAChange,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/domains", s.handleDomains)
	mux.HandleFunc("/api/entities", s.handleEntities)
	mux.HandleFunc("/api/exposed", s.handleExposed)
	mux.HandleFunc("/api/homekit/setup", s.handleHomeKitSetup)
	mux.HandleFunc("/api/home_assistant/config", s.handleHAConfig)
	mux.HandleFunc("/api/home_assistant/test", s.handleHATest)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st := s.snapshotStatus()
	writeJSON(w, st)
}

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{
		"domains": mapper.ExposableDomains(),
	})
}

func (s *Server) snapshotStatus() map[string]interface{} {
	s.mu.RLock()
	cfg := *s.cfg
	haSvc := *s.ha
	br := *s.bridge
	s.mu.RUnlock()

	out := map[string]interface{}{
		"ha_configured": cfg.HAConfigured(),
		"ha_url":        cfg.HomeAssistant.URL,
		"ui_listen":     cfg.UI.Listen,
		"bridge_name":   cfg.Bridge.Name,
		"exposed_count": len(s.exposure.EntityIDs()),
	}
	if haSvc != nil {
		out["ha_connected"] = haSvc.Connected()
		if err := haSvc.LastError(); err != nil {
			out["ha_error"] = err.Error()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if c, err := haSvc.GetConfig(ctx); err == nil {
			out["ha_version"] = c.Version
		}
	}
	if br != nil {
		out["homekit_paired"] = br.IsPaired()
		out["bridged_devices"] = br.DeviceCount()
	}
	return out
}

func (s *Server) handleEntities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	haSvc := *s.ha
	cfg := *s.cfg
	s.mu.RUnlock()

	if haSvc == nil || !cfg.HAConfigured() {
		http.Error(w, "home assistant not configured", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	states, err := haSvc.States(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	names := map[string]string{}
	entityAreas := map[string]string{}
	areaNames := map[string]string{}
	if reg, err := haSvc.EntityRegistry(ctx); err == nil {
		names = haclient.FriendlyNames(reg, states)
		entityAreas = haclient.EntityAreaIDs(reg)
	}
	if an, err := haSvc.AreaNamesByID(ctx); err == nil {
		areaNames = an
	}
	roomNaming := haclient.RoomNaming(cfg.Bridge.RoomNaming)
	exposed := s.exposure.ExposedSet()
	domainFilter := r.URL.Query().Get("domain")
	exposureFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("exposure")))
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	var list []map[string]interface{}
	for _, se := range states {
		if !mapper.IsExposable(se) {
			continue
		}
		domain := mapper.Domain(se.EntityID)
		if domainFilter != "" && domain != domainFilter {
			continue
		}
		friendly := haclient.DisplayName(se.EntityID, names)
		defaultHomeKitName := haclient.AccessoryDisplayName(se.EntityID, friendly, entityAreas[se.EntityID], areaNames, roomNaming)
		override := s.exposure.HomeKitNameOverride(se.EntityID)
		homekitName := defaultHomeKitName
		if strings.TrimSpace(override) != "" {
			homekitName = strings.TrimSpace(override)
		}
		areaLabel := ""
		if aid := entityAreas[se.EntityID]; aid != "" {
			areaLabel = areaNames[aid]
		}
		if q != "" && !strings.Contains(strings.ToLower(se.EntityID), q) &&
			!strings.Contains(strings.ToLower(friendly), q) &&
			!strings.Contains(strings.ToLower(homekitName), q) {
			continue
		}
		_, exp := exposed[se.EntityID]
		switch exposureFilter {
		case "enabled", "on", "exposed":
			if !exp {
				continue
			}
		case "disabled", "off", "hidden":
			if exp {
				continue
			}
		}
		list = append(list, map[string]interface{}{
			"entity_id":              se.EntityID,
			"name":                   friendly,
			"homekit_name":           homekitName,
			"homekit_name_default":   defaultHomeKitName,
			"homekit_name_override":  override,
			"area":                   areaLabel,
			"domain":                   domain,
			"state":                  se.State,
			"exposed":                exp,
		})
	}
	writeJSON(w, map[string]interface{}{"entities": list})
}

func (s *Server) handleExposed(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]interface{}{
			"entity_ids": s.exposure.EntityIDs(),
			"entries":    s.exposure.Entries(),
		})
	case http.MethodPut:
		var body struct {
			EntityIDs     []string          `json:"entity_ids"`
			HomeKitNames  map[string]string `json:"homekit_names"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, id := range body.EntityIDs {
			if !mapper.IsMappable(id) {
				http.Error(w, "unsupported entity: "+id, http.StatusBadRequest)
				return
			}
		}
		if err := s.exposure.SetExposure(body.EntityIDs, body.HomeKitNames); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.mu.RLock()
		br := *s.bridge
		s.mu.RUnlock()
		if br != nil {
			br.Reload()
		}
		writeJSON(w, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHomeKitSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	cfg := *s.cfg
	br := *s.bridge
	s.mu.RUnlock()

	pin := cfg.EffectivePin()
	uri, err := homekit.SetupURI(pin, homekit.CategoryBridge, cfg.Bridge.SetupID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	png, err := homekit.QRPNG(uri, 320)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	paired := false
	if br != nil {
		paired = br.IsPaired()
	}
	writeJSON(w, map[string]interface{}{
		"pin":         homekit.FormatPIN(pin),
		"setup_uri":   uri,
		"qr_png_base64": base64.StdEncoding.EncodeToString(png),
		"paired":      paired,
		"bridge_name": cfg.Bridge.Name,
	})
}

func (s *Server) handleHAConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		cfg := *s.cfg
		s.mu.RUnlock()
		writeJSON(w, map[string]interface{}{
			"url":   cfg.HomeAssistant.URL,
			"token_set": strings.TrimSpace(cfg.HomeAssistant.Token) != "" ||
				strings.TrimSpace(getEnvToken()) != "",
		})
	case http.MethodPut:
		var body struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.cfg.HomeAssistant.URL = strings.TrimSpace(body.URL)
		if strings.TrimSpace(body.Token) != "" {
			s.cfg.HomeAssistant.Token = strings.TrimSpace(body.Token)
		}
		cfgCopy := *s.cfg
		s.mu.Unlock()
		if err := config.Save(s.cfgPath, cfgCopy); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("config saved to %s", s.cfgPath)
		if s.onHAChange != nil {
			s.onHAChange()
		}
		writeJSON(w, map[string]interface{}{
			"ok":        true,
			"config_path": s.cfgPath,
			"token_set": strings.TrimSpace(cfgCopy.HomeAssistant.Token) != "" ||
				strings.TrimSpace(getEnvToken()) != "",
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHATest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	url := strings.TrimSpace(body.URL)
	token := strings.TrimSpace(body.Token)
	if token == "" {
		s.mu.RLock()
		token = s.cfg.HomeAssistant.Token
		if token == "" {
			token = getEnvToken()
		}
		s.mu.RUnlock()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := haclient.ValidateConnection(ctx, url, token); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func getEnvToken() string {
	return strings.TrimSpace(os.Getenv("HAHELPER_HA_TOKEN"))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("json encode: %v", err)
	}
}
