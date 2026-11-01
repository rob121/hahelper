package bridge

import (
	"context"
	"log"
	"time"

	ha "github.com/mkelcik/go-ha-client/v2"
	"github.com/rob121/hahelper/internal/haclient"
	"github.com/rob121/hahelper/internal/mapper"
)

const (
	defaultStatePollInterval = 90 * time.Second
	watchRetryDelay          = 2 * time.Second
)

func (m *Manager) statePollInterval() time.Duration {
	if m.cfg == nil {
		return defaultStatePollInterval
	}
	sec := m.cfg.HAP.StatePollSeconds
	if sec < 0 {
		return 0
	}
	if sec == 0 {
		return defaultStatePollInterval
	}
	if sec < 15 {
		sec = 15
	}
	return time.Duration(sec) * time.Second
}

func (m *Manager) syncDevicesFromHA(ctx context.Context, devices []*mapper.Device) {
	if m.ha == nil || len(devices) == 0 {
		return
	}
	states, err := m.ha.StatesREST(ctx)
	if err != nil {
		log.Printf("ha state sync: %v", err)
		return
	}
	byID := haclient.StateMap(states)
	for _, d := range devices {
		if se, ok := byID[d.EntityID]; ok {
			d.ApplyHA(se)
		}
	}
}

func (m *Manager) pollHA(ctx context.Context, devices []*mapper.Device) {
	interval := m.statePollInterval()
	if interval == 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			m.syncDevicesFromHA(syncCtx, devices)
			cancel()
		}
	}
}

func (m *Manager) watchHA(ctx context.Context, devices []*mapper.Device) {
	if len(devices) == 0 || m.ha == nil {
		return
	}
	ids := make([]string, len(devices))
	byID := make(map[string]*mapper.Device, len(devices))
	for i, d := range devices {
		ids[i] = d.EntityID
		byID[d.EntityID] = d
	}

	for ctx.Err() == nil {
		syncCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		m.syncDevicesFromHA(syncCtx, devices)
		cancel()

		sub, err := m.ha.WS.SubscribeStateChangedMany(ctx, ids...)
		if err != nil {
			log.Printf("ha subscribe: %v", err)
			if !sleepCtx(ctx, watchRetryDelay) {
				return
			}
			continue
		}

		subDone := m.runStateSubscription(ctx, sub, byID)
		_ = sub.Unsubscribe(context.Background())
		if subDone {
			return
		}
		log.Printf("ha state subscription ended; resubscribing in %s", watchRetryDelay)
		if !sleepCtx(ctx, watchRetryDelay) {
			return
		}
	}
}

func (m *Manager) runStateSubscription(ctx context.Context, sub *ha.WSSubscription, byID map[string]*mapper.Device) (done bool) {
	for {
		select {
		case <-ctx.Done():
			return true
		case err, ok := <-sub.Errors():
			if !ok {
				return false
			}
			if err != nil {
				log.Printf("ha subscription: %v", err)
			}
			return false
		case ev, ok := <-sub.Events():
			if !ok {
				return false
			}
			data, ok, err := ev.StateChanged()
			if err != nil || !ok || data.NewState == nil {
				continue
			}
			dev, found := byID[data.EntityID]
			if !found {
				continue
			}
			se := ha.StateEntity{
				EntityID:   data.EntityID,
				State:      data.NewState.State,
				Attributes: data.NewState.Attributes,
			}
			dev.ApplyHA(se)
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
