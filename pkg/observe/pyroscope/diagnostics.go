//go:build pyroscope

package pyroscope

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/formancehq/go-libs/v5/pkg/observe/redact"
)

// scrubber masks configured credentials before SDK callbacks reach the logger.
// An ad-hoc SDK address override is included in the diagnostic policy too.
func (c Config) scrubber() func(string) string {
	secrets := []string{c.AuthToken, c.BasicAuthUser, c.BasicAuthPassword}
	if c.BasicAuthUser != "" || c.BasicAuthPassword != "" {
		secrets = append(secrets, base64.StdEncoding.EncodeToString([]byte(c.BasicAuthUser+":"+c.BasicAuthPassword)))
	}
	addresses := []string{c.ServerAddress, os.Getenv("PYROSCOPE_ADHOC_SERVER_ADDRESS")}
	replacements := []string{}
	for _, address := range addresses {
		if address == "" {
			continue
		}
		if parsed, err := url.Parse(address); err == nil {
			if parsed.User != nil {
				secrets = append(secrets, parsed.User.Username())
				if password, ok := parsed.User.Password(); ok {
					secrets = append(secrets, password)
				}
			}
			for key, values := range parsed.Query() {
				if redact.SensitiveHeader(key) {
					secrets = append(secrets, values...)
				}
			}
			replacements = append(replacements, address, redact.URL(parsed))
		} else {
			replacements = append(replacements, address, redact.Marker)
		}
	}
	for key, value := range c.HTTPHeaders {
		if redact.SensitiveHeader(key) {
			secrets = append(secrets, value)
		}
	}
	for key, value := range c.Tags {
		if redact.SensitiveHeader(key) {
			secrets = append(secrets, value)
		}
	}
	expanded := make([]string, 0, len(secrets)*3)
	for _, value := range secrets {
		if value != "" {
			quoted := strconv.Quote(value)
			encoded, _ := json.Marshal(value)
			expanded = append(expanded, value, url.QueryEscape(value), url.PathEscape(value), quoted[1:len(quoted)-1], string(encoded[1:len(encoded)-1]))
		}
	}
	slices.SortFunc(expanded, func(a, b string) int { return len(b) - len(a) })
	return func(message string) string {
		if len(replacements) > 0 {
			message = strings.NewReplacer(replacements...).Replace(message)
		}
		for _, value := range expanded {
			message = strings.ReplaceAll(message, value, redact.Marker)
		}
		return string(redact.Bytes([]byte(message)))
	}
}

// MarshalJSON exposes diagnostic settings without native SDK clients, logger
// objects or credentials. Logging c.Config (the raw SDK value) is not protected.
func (c Config) MarshalJSON() ([]byte, error) {
	scrub := c.scrubber()
	return json.Marshal(map[string]any{
		"enabled": c.Enabled, "application_name": scrub(c.ApplicationName), "server_address": scrub(c.ServerAddress),
		"tags": scrub(fmt.Sprint(c.Tags)), "profile_types": c.ProfileTypes, "upload_rate": c.UploadRate,
		"mutex_profile_fraction": c.MutexProfileFraction, "block_profile_rate": c.BlockProfileRate,
		"disable_gc_runs": c.DisableGCRuns, "tag_spans": c.TagSpans, "tag_all_spans": c.TagAllSpans,
	})
}
func (c Config) String() string   { data, _ := c.MarshalJSON(); return string(data) }
func (c Config) GoString() string { return c.String() }
