package bridge

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	ha "github.com/mkelcik/go-ha-client/v2"
	"github.com/rob121/hahelper/internal/config"
	"github.com/rob121/hahelper/internal/exposure"
	"github.com/rob121/hahelper/internal/haclient"
	"github.com/rob121/hahelper/internal/mapper"
)

// Manager runs the HAP bridge and keeps it in sync with Home Assistant.
type Manager struct {
	cfg      *config.Config
	ha       *haclient.Service
	exposure *exposure.Registry

	mu          sync.Mutex
	server      *hap.Server
	stopCancel  context.CancelFunc
	parentCtx   context.Context
	wantReload  bool
	devices     []*mapper.Device
	guard       mapper.SyncGuard
}

func NewManager(cfg *config.Config, haSvc *haclient.Service, exp *exposure.Registry) *Manager {
	return &Manager{cfg: cfg, ha: haSvc, exposure: exp}
}

// SetHA updates the Home Assistant client (may be nil) and reloads the bridge.
func (m *Manager) SetHA(haSvc *haclient.Service) {
	m.mu.Lock()
	m.ha = haSvc
	m.mu.Unlock()
	m.Reload()
}

func (m *Manager) Server() *hap.Server {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.server
}

func (m *Manager) IsPaired() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server == nil {
		return false
	}
	return m.server.IsPaired()
}

func (m *Manager) DeviceCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.devices)
}

// Start runs the bridge until parent ctx is cancelled.
func (m *Manager) Start(parent context.Context) {
	m.parentCtx = parent
	go m.serveLoop()
}

// Reload restarts HAP with the latest exposure list.
func (m *Manager) Reload() {
	m.mu.Lock()
	m.wantReload = true
	cancel := m.stopCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) serveLoop() {
	for {
		if m.parentCtx.Err() != nil {
			return
		}
		ctx, cancel := context.WithCancel(m.parentCtx)
		m.mu.Lock()
		m.stopCancel = cancel
		m.wantReload = false
		m.mu.Unlock()

		err := m.run(ctx)
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
			log.Printf("hap bridge: %v", err)
		}

		m.mu.Lock()
		reload := m.wantReload
		m.mu.Unlock()
		if m.parentCtx.Err() != nil {
			return
		}
		if !reload {
			return
		}
	}
}

func (m *Manager) run(ctx context.Context) error {
	cfg := *m.cfg
	var states ha.StateEntities
	stateByID := map[string]ha.StateEntity{}
	names := map[string]string{}
	entityAreas := map[string]string{}
	areaNames := map[string]string{}
	roomNaming := haclient.RoomNaming(cfg.Bridge.RoomNaming)
	if m.ha != nil {
		var err error
		states, err = m.ha.States(ctx)
		if err != nil {
			return err
		}
		stateByID = haclient.StateMap(states)
		if reg, err := m.ha.EntityRegistry(ctx); err == nil {
			names = haclient.FriendlyNames(reg, states)
			entityAreas = haclient.EntityAreaIDs(reg)
		}
		if an, err := m.ha.AreaNamesByID(ctx); err == nil {
			areaNames = an
		}
	}

	bridgeInfo := cfg.Bridge
	br := accessory.NewBridge(accessory.Info{
		Name:         bridgeInfo.Name,
		SerialNumber: bridgeInfo.SerialNumber,
		Manufacturer: bridgeInfo.Manufacturer,
		Model:        bridgeInfo.Model,
		Firmware:     bridgeInfo.Firmware,
	})
	br.A.Id = 1

	var accessories []*accessory.A
	accessories = append(accessories, br.A)

	var devices []*mapper.Device
	for _, exp := range m.exposure.Entries() {
		entry := exp.EntityID
		if !mapper.IsMappable(entry) {
			continue
		}
		var sePtr *ha.StateEntity
		if se, ok := stateByID[entry]; ok {
			seCopy := se
			sePtr = &seCopy
		}
		friendly := haclient.DisplayName(entry, names)
		defaultName := haclient.AccessoryDisplayName(entry, friendly, entityAreas[entry], areaNames, roomNaming)
		name := m.exposure.ResolveHomeKitName(entry, defaultName)
		hid := exp.HAPID
		if hid == 0 {
			hid = 2
		}
		dev, err := mapper.Build(entry, name, hid, sePtr, m.ha, &m.guard)
		if err != nil {
			log.Printf("skip %s: %v", entry, err)
			continue
		}
		devices = append(devices, dev)
		accessories = append(accessories, dev.A)
	}

	store := hap.NewFsStore(cfg.HAP.StorePath)
	var srv *hap.Server
	var err error
	if len(accessories) == 1 {
		srv, err = hap.NewServer(store, accessories[0])
	} else {
		srv, err = hap.NewServer(store, accessories[0], accessories[1:]...)
	}
	if err != nil {
		return err
	}
	srv.Pin = cfg.EffectivePin()
	srv.SetupId = cfg.Bridge.SetupID
	if cfg.HAP.Addr != "" {
		srv.Addr = cfg.HAP.Addr
	}

	m.mu.Lock()
	m.server = srv
	m.devices = devices
	m.mu.Unlock()

	go m.watchHA(ctx, devices)
	go m.pollHA(ctx, devices)

	err = srv.ListenAndServe(ctx)
	m.mu.Lock()
	m.server = nil
	m.mu.Unlock()
	return err
}

