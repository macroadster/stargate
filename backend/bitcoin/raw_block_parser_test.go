package bitcoin

import (
	"bytes"
	"testing"
)

func encodePush(data []byte) []byte {
	l := len(data)
	switch {
	case l <= 75:
		return append([]byte{byte(l)}, data...)
	case l <= 0xff:
		return append([]byte{0x4c, byte(l)}, data...)
	case l <= 0xffff:
		return append([]byte{0x4d, byte(l), byte(l >> 8)}, data...)
	default:
		return append([]byte{0x4e, byte(l), byte(l >> 8), byte(l >> 16), byte(l >> 24)}, data...)
	}
}

func buildOrdinalScript(contentType string, payload []byte) []byte {
	script := []byte{0x00, 0x63}                          // OP_FALSE OP_IF
	script = append(script, encodePush([]byte("ord"))...) // "ord" marker
	script = append(script, encodePush([]byte{0x01})...)  // version push
	script = append(script, encodePush([]byte(contentType))...)
	script = append(script, byte(0x00)) // OP_0 separator
	script = append(script, encodePush(payload)...)
	script = append(script, byte(0x68)) // OP_ENDIF
	return script
}

func TestParseOrdinalsExtractsPayloadWithoutPushdataPrefixes(t *testing.T) {
	payload := []byte("hello world")
	script := buildOrdinalScript("text/plain;charset=utf-8", payload)

	contentType, content, ok := parseOrdinals(script)
	if !ok {
		t.Fatalf("expected ordinals payload to be detected")
	}
	if contentType != "text/plain;charset=utf-8" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if !bytes.Equal(content, payload) {
		t.Fatalf("content mismatch, got: %x", content)
	}
	if bytes.HasPrefix(content, []byte{0x4c, 0x4d, 0x4e}) {
		t.Fatalf("content still includes pushdata prefix: %x", content[:2])
	}
}

func TestParseOrdinalsHandlesPushdata2Bodies(t *testing.T) {
	payload := bytes.Repeat([]byte("A"), 0x0103) // forces OP_PUSHDATA2
	script := buildOrdinalScript("text/plain", payload)

	contentType, content, ok := parseOrdinals(script)
	if !ok {
		t.Fatalf("expected ordinals payload to be detected")
	}
	if contentType != "text/plain" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if len(content) != len(payload) {
		t.Fatalf("unexpected payload length: %d", len(content))
	}
	if content[0] != 'A' || content[len(content)-1] != 'A' {
		t.Fatalf("payload data corrupted")
	}
}
