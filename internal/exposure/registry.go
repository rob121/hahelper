package exposure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Entry struct {
	EntityID     string `json:"entity_id"`
	HAPID        int    `json:"hap_id"`
	HomeKitName  string `json:"homekit_name,omitempty"`
}

type File struct {
	SchemaVersion int     `json:"schema_version"`
	NextID        int     `json:"next_id"`
	Entries       []Entry `json:"entries"`
}

type Registry struct {
	path string
	mu   sync.RWMutex
	data File
}

func Load(storePath string) (*Registry, error) {
	path := filepath.Join(storePath, "exposed.json")
	r := &Registry{path: path, data: File{NextID: 2}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			migrate(&r.data)
			return r, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &r.data); err != nil {
		return nil, err
	}
	if r.data.NextID < 2 {
		r.data.NextID = 2
	}
	prevVer := r.data.SchemaVersion
	migrate(&r.data)
	if r.data.SchemaVersion != prevVer {
		if err := r.Save(); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) Save() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.saveLocked()
}

func (r *Registry) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, len(r.data.Entries))
	copy(out, r.data.Entries)
	return out
}

func (r *Registry) EntityIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.data.Entries))
	for _, e := range r.data.Entries {
		out = append(out, e.EntityID)
	}
	return out
}

func (r *Registry) HAPID(entityID string) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.data.Entries {
		if e.EntityID == entityID {
			return e.HAPID, true
		}
	}
	return 0, false
}

func (r *Registry) HomeKitNameOverride(entityID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.data.Entries {
		if e.EntityID == entityID {
			return e.HomeKitName
		}
	}
	return ""
}

// ResolveHomeKitName returns the override when set, otherwise defaultName.
func (r *Registry) ResolveHomeKitName(entityID, defaultName string) string {
	if o := strings.TrimSpace(r.HomeKitNameOverride(entityID)); o != "" {
		return o
	}
	return defaultName
}

func (r *Registry) SetEntityIDs(ids []string) error {
	return r.SetExposure(ids, nil)
}

// SetExposure updates the exposed entity list and optional HomeKit name overrides.
// homekitNames maps entity_id -> override; empty string clears an override.
// Keys not present leave existing overrides unchanged.
func (r *Registry) SetExposure(ids []string, homekitNames map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	keep := make(map[string]Entry, len(r.data.Entries))
	for _, e := range r.data.Entries {
		keep[e.EntityID] = e
	}

	var next []Entry
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}

		e, ok := keep[id]
		if !ok {
			hid := r.data.NextID
			r.data.NextID++
			e = Entry{EntityID: id, HAPID: hid}
		}
		if homekitNames != nil {
			if name, ok := homekitNames[id]; ok {
				e.HomeKitName = strings.TrimSpace(name)
			}
		}
		next = append(next, e)
	}
	r.data.Entries = next
	migrate(&r.data)
	return r.saveLocked()
}

func (r *Registry) saveLocked() error {
	migrate(&r.data)
	b, err := json.MarshalIndent(r.data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(r.path, b, 0640)
}

func (r *Registry) ExposedSet() map[string]struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := make(map[string]struct{}, len(r.data.Entries))
	for _, e := range r.data.Entries {
		m[e.EntityID] = struct{}{}
	}
	return m
}
