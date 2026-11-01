package exposure

import "testing"

func TestMigrate_addsSchemaVersion(t *testing.T) {
	f := &File{NextID: 2, Entries: []Entry{{EntityID: "light.k", HAPID: 2}}}
	migrate(f)
	if f.SchemaVersion != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", f.SchemaVersion, currentSchemaVersion)
	}
}

func TestMigrate_idempotent(t *testing.T) {
	f := &File{SchemaVersion: currentSchemaVersion}
	migrate(f)
	if f.SchemaVersion != currentSchemaVersion {
		t.Fatalf("schema changed")
	}
}
