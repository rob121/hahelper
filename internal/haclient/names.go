package haclient

import (
	"context"
	"strings"
	"unicode"

	ha "github.com/mkelcik/go-ha-client/v2"
)

// EntityAreaIDs maps entity_id -> area_id from the display entity registry ("ai" field).
func EntityAreaIDs(reg ha.DisplayEntityRegistry) map[string]string {
	out := make(map[string]string, len(reg.Entities))
	for _, ent := range reg.Entities {
		id, _ := ent["ei"].(string)
		if id == "" {
			continue
		}
		if ai, ok := ent["ai"].(string); ok && ai != "" {
			out[id] = ai
		}
	}
	return out
}

// AreaNamesByID fetches area_id -> human name from Home Assistant.
func (s *Service) AreaNamesByID(ctx context.Context) (map[string]string, error) {
	var areas []struct {
		AreaID string `json:"area_id"`
		Name   string `json:"name"`
	}
	err := s.WS.Do(ctx, map[string]interface{}{
		"type": "config/area_registry/list",
	}, &areas)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(areas))
	for _, a := range areas {
		if a.AreaID != "" && a.Name != "" {
			out[a.AreaID] = a.Name
		}
	}
	return out, nil
}

// RoomNaming controls how HomeKit accessory names include room/area hints.
type RoomNaming string

const (
	RoomNamingArea     RoomNaming = "area"      // HA area + friendly name
	RoomNamingEntityID RoomNaming = "entity_id"   // infer room token from entity_id prefix
	RoomNamingBoth     RoomNaming = "both"      // area if set, else entity_id heuristic
	RoomNamingNone     RoomNaming = "none"      // friendly name only
)

// AccessoryDisplayName builds the name shown in the Home app (HAP cannot assign rooms).
func AccessoryDisplayName(entityID, friendlyName string, entityAreaID string, areaNames map[string]string, mode RoomNaming) string {
	friendlyName = strings.TrimSpace(friendlyName)
	if friendlyName == "" {
		friendlyName = humanizeObjectID(entityID)
	}

	switch mode {
	case RoomNamingNone:
		return friendlyName
	}

	room := ""
	if mode == RoomNamingArea || mode == RoomNamingBoth {
		if entityAreaID != "" {
			room = areaNames[entityAreaID]
		}
	}
	if room == "" && (mode == RoomNamingEntityID || mode == RoomNamingBoth) {
		room = inferRoomFromEntityID(entityID, areaNames)
	}
	if room == "" {
		return friendlyName
	}
	if strings.HasPrefix(strings.ToLower(friendlyName), strings.ToLower(room)) {
		return friendlyName
	}
	return room + " · " + friendlyName
}

func inferRoomFromEntityID(entityID string, areaNames map[string]string) string {
	_, objectID, err := ha.ParseEntityID(entityID)
	if err != nil {
		return ""
	}
	parts := strings.Split(objectID, "_")
	if len(parts) < 2 {
		return ""
	}
	slug := parts[0]
	for _, name := range areaNames {
		if slugMatchesArea(slug, name) {
			return name
		}
	}
	return titleWords(slug)
}

func slugMatchesArea(slug, areaName string) bool {
	return normalizeSlug(areaName) == normalizeSlug(slug)
}

func normalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '_' || r == '-' {
			// skip
		}
	}
	return b.String()
}

func humanizeObjectID(entityID string) string {
	_, objectID, err := ha.ParseEntityID(entityID)
	if err != nil {
		return entityID
	}
	return titleWords(strings.ReplaceAll(objectID, "_", " "))
}

func titleWords(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		runes := []rune(strings.ToLower(w))
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
