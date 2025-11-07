package audit

import (
	"strings"

	"github.com/formancehq/go-libs/v3/publish"
	"github.com/spf13/cobra"
)

// BuildAuditTopic constructs the audit topic from the publisher's wildcard mapping.
// This ensures audit logs are automatically routed to the correct stream without manual configuration.
//
// The function parses the PUBLISHER_TOPIC_MAPPING flag and derives the audit topic by replacing
// the last segment (after the last separator) with "audit".
//
// Supported separators: ".", "-", "_"
//
// Examples:
//   - "*:organization-stack.ledger"  → "organization-stack.audit"
//   - "*:organization-stack-ledger"  → "organization-stack-audit"
//   - "*:organization_stack_ledger"  → "organization_stack_audit"
//   - "*:payments"                   → "payments_audit"
//   - No wildcard mapping            → "AUDIT" (fallback)
//
// This function is designed to work seamlessly with NATS JetStream, Kafka, and other message brokers.
func BuildAuditTopic(cmd *cobra.Command) string {
	topicMappings, _ := cmd.Flags().GetStringSlice(publish.PublisherTopicMappingFlag)

	for _, mapping := range topicMappings {
		parts := strings.SplitN(mapping, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Found wildcard mapping like "*:organization-stack.ledger"
		if key == "*" {
			return deriveAuditTopic(value)
		}
	}

	// No wildcard mapping found, use default
	return "AUDIT"
}

// BuildAuditTopicFromMapping constructs the audit topic from a raw topic mapping string.
// This is useful when you have the mapping string directly without access to cobra.Command.
//
// Examples:
//   - "*:organization-stack.ledger"  → "organization-stack.audit"
//   - "organization-stack.ledger"    → "organization-stack.audit"
func BuildAuditTopicFromMapping(mapping string) string {
	parts := strings.SplitN(mapping, ":", 2)

	// If mapping has colon format, extract and use the value part
	if len(parts) == 2 {
		value := strings.TrimSpace(parts[1])
		return deriveAuditTopic(value)
	}

	// Treat entire string as base topic
	return deriveAuditTopic(mapping)
}

// deriveAuditTopic replaces the last segment after a separator with "audit"
// Handles multiple separator types: ".", "-", "_"
// Separators at index 0 (e.g., ".ledger") are handled and will be replaced to produce ".audit"
func deriveAuditTopic(baseTopic string) string {
	separators := []string{".", "-", "_"}
	lastIndex := -1
	lastSep := ""

	// Find the last occurrence of any separator
	for _, sep := range separators {
		if idx := strings.LastIndex(baseTopic, sep); idx > lastIndex {
			lastIndex = idx
			lastSep = sep
		}
	}

	// If a separator was found (including at index 0), replace the last segment
	if lastIndex >= 0 {
		return baseTopic[:lastIndex] + lastSep + "audit"
	}

	// No separator found, append "_audit"
	return baseTopic + "_audit"
}
