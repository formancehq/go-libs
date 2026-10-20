//go:build pyroscope

package pyroscope

import "encoding/json"

// MarshalJSON exposes only numeric and boolean diagnostic settings. Logging
// c.Config (the raw SDK value) bypasses this projection and is unsafe.
func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"enabled": c.Enabled, "upload_rate": c.UploadRate,
		"mutex_profile_fraction": c.MutexProfileFraction, "block_profile_rate": c.BlockProfileRate,
		"disable_gc_runs": c.DisableGCRuns, "tag_spans": c.TagSpans, "tag_all_spans": c.TagAllSpans,
	})
}
func (c Config) String() string   { data, _ := c.MarshalJSON(); return string(data) }
func (c Config) GoString() string { return c.String() }
