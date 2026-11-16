package exposure

const currentSchemaVersion = 3

// migrate upgrades on-disk exposure data to the latest schema.
func migrate(f *File) {
	if f == nil {
		return
	}
	if f.SchemaVersion >= currentSchemaVersion {
		return
	}
	// v1: entity_id + hap_id only (schema_version absent or 0).
	// v2: optional homekit_name per entry.
	// v3: optional stream_source (RTSP) for cameras.
	f.SchemaVersion = currentSchemaVersion
}