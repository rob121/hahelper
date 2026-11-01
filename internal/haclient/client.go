package haclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	ha "github.com/mkelcik/go-ha-client/v2"
)

// Service wraps REST + WebSocket access to Home Assistant.
type Service struct {
	REST *ha.Client
	WS   *ha.WSClient

	mu      sync.Mutex
	connected bool
	lastErr error
}

func New(url, token string, wsReconnect bool) (*Service, error) {
	rest, err := ha.NewClient(url,
		ha.WithToken(token),
		ha.WithTimeout(30*time.Second),
	)
	if err != nil {
		return nil, err
	}
	opts := []ha.WSOption{}
	if wsReconnect {
		opts = append(opts,
			ha.WithAutoReconnect(true),
			ha.WithMaxRetries(0),
			ha.WithReconnectBackoff(time.Second, 30*time.Second),
		)
	}
	ws := rest.WS(opts...)
	return &Service{REST: rest, WS: ws}, nil
}

func (s *Service) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.WS.Connect(ctx)
	s.connected = err == nil
	s.lastErr = err
	return err
}

func (s *Service) Close() {
	s.WS.Close()
	s.mu.Lock()
	s.connected = false
	s.mu.Unlock()
}

func (s *Service) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.WS != nil && s.WS.IsConnected() {
		return true
	}
	return s.connected
}

func (s *Service) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *Service) Ping(ctx context.Context) error {
	return s.REST.Ping(ctx)
}

func (s *Service) GetConfig(ctx context.Context) (ha.Config, error) {
	return s.REST.GetConfig(ctx)
}

func (s *Service) States(ctx context.Context) (ha.StateEntities, error) {
	return s.WS.GetStates(ctx)
}

// StatesREST returns entity states via the REST API (works without an active WS subscription).
func (s *Service) StatesREST(ctx context.Context) (ha.StateEntities, error) {
	return s.REST.GetStates(ctx)
}

func (s *Service) EntityRegistry(ctx context.Context) (ha.DisplayEntityRegistry, error) {
	return s.WS.ListEntityRegistryForDisplay(ctx)
}

func (s *Service) CallEntity(ctx context.Context, domain, service, entityID string, data map[string]interface{}) error {
	_, err := s.WS.CallServiceForEntity(ctx, domain, service, entityID, data)
	return err
}

// FriendlyNames builds entity_id -> display name from registry + states.
func FriendlyNames(reg ha.DisplayEntityRegistry, states ha.StateEntities) map[string]string {
	names := make(map[string]string, len(states))
	for _, se := range states {
		if n := attrString(se.Attributes, "friendly_name"); n != "" {
			names[se.EntityID] = n
		}
	}
	for _, ent := range reg.Entities {
		id, _ := ent["ei"].(string)
		if id == "" {
			continue
		}
		if n, ok := ent["nn"].(string); ok && n != "" {
			names[id] = n
		}
	}
	return names
}

func attrString(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	v, ok := attrs[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func DisplayName(entityID string, names map[string]string) string {
	if n, ok := names[entityID]; ok && n != "" {
		return n
	}
	_, obj, err := ha.ParseEntityID(entityID)
	if err != nil {
		return entityID
	}
	return obj
}

func StateMap(states ha.StateEntities) map[string]ha.StateEntity {
	m := make(map[string]ha.StateEntity, len(states))
	for _, se := range states {
		m[se.EntityID] = se
	}
	return m
}

func ValidateConnection(ctx context.Context, url, token string) error {
	svc, err := New(url, token, false)
	if err != nil {
		return err
	}
	defer svc.Close()
	if err := svc.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	if err := svc.Connect(ctx); err != nil {
		return fmt.Errorf("websocket: %w", err)
	}
	return nil
}
