package stego

import (
	"fmt"
	"strconv"
	"strings"
)

type Manifest struct {
	SchemaVersion    int
	ContractID       string
	ProposalID       string
	VisiblePixelHash string
	PayloadCID       string
	TasksCID         string
	CreatedAt        int64
	Issuer           string
}

func BuildManifestYAML(m Manifest) ([]byte, error) {
	if m.SchemaVersion <= 0 {
		return nil, fmt.Errorf("schema_version must be set")
	}
	if m.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id must be set")
	}
	if m.VisiblePixelHash == "" {
		return nil, fmt.Errorf("visible_pixel_hash must be set")
	}
	if m.PayloadCID == "" {
		return nil, fmt.Errorf("payload_cid must be set")
	}
	if m.CreatedAt <= 0 {
		return nil, fmt.Errorf("created_at must be set")
	}
	if m.Issuer == "" {
		return nil, fmt.Errorf("issuer must be set")
	}

	var b strings.Builder
	writeField(&b, "schema_version", strconv.Itoa(m.SchemaVersion))
	if m.ContractID != "" {
		writeField(&b, "contract_id", formatYAMLValue(m.ContractID))
	}
	writeField(&b, "proposal_id", formatYAMLValue(m.ProposalID))
	writeField(&b, "visible_pixel_hash", formatYAMLValue(m.VisiblePixelHash))
	writeField(&b, "payload_cid", formatYAMLValue(m.PayloadCID))
	if m.TasksCID != "" {
		writeField(&b, "tasks_cid", formatYAMLValue(m.TasksCID))
	}
	writeField(&b, "created_at", strconv.FormatInt(m.CreatedAt, 10))
	writeField(&b, "issuer", formatYAMLValue(m.Issuer))

	return []byte(b.String()), nil
}

func writeField(b *strings.Builder, key string, value string) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
}

func formatYAMLValue(value string) string {
	if value == "" {
		return "\"\""
	}
	if needsQuotes(value) {
		escaped := strings.ReplaceAll(value, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		return "\"" + escaped + "\""
	}
	return value
}

func needsQuotes(value string) bool {
	if strings.TrimSpace(value) != value {
		return true
	}
	for _, ch := range value {
		switch ch {
		case ':', '#', '\n', '\r', '\t':
			return true
		}
	}
	return false
}
