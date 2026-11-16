package exposure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetExposure_preservesAndClearsHomeKitName(t *testing.T) {
	dir := t.TempDir()
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetExposure([]string{"light.a"}, map[string]string{"light.a": "Custom Lamp"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.HomeKitNameOverride("light.a"); got != "Custom Lamp" {
		t.Fatalf("override = %q", got)
	}
	if err := r.SetExposure([]string{"light.a"}, map[string]string{"light.a": ""}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.HomeKitNameOverride("light.a"); got != "" {
		t.Fatalf("expected cleared override, got %q", got)
	}
}

func TestSetExposure_streamSource(t *testing.T) {
	dir := t.TempDir()
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	rtsp := "rtsp://cam/stream"
	if err := r.SetExposure([]string{"camera.garage"}, nil, map[string]string{"camera.garage": rtsp}); err != nil {
		t.Fatal(err)
	}
	if got := r.StreamSource("camera.garage"); got != rtsp {
		t.Fatalf("stream_source = %q", got)
	}
	if err := r.SetExposure([]string{"camera.garage"}, nil, map[string]string{"camera.garage": ""}); err != nil {
		t.Fatal(err)
	}
	if got := r.StreamSource("camera.garage"); got != "" {
		t.Fatalf("expected cleared stream_source, got %q", got)
	}
}

func TestLoad_migratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exposed.json")
	legacy := `{
  "next_id": 3,
  "entries": [
    {"entity_id": "switch.k", "hap_id": 2}
  ]
}
`
	if err := os.WriteFile(path, []byte(legacy), 0640); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, `"schema_version"`) {
		t.Fatalf("expected migrated file to include schema_version: %s", b)
	}
	if !strings.Contains(body, `"schema_version": 3`) {
		t.Fatalf("expected schema_version 3: %s", b)
	}
}
