package bitcoin

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// validateIngestedBlock is a cheap application-level sanity check before the
// monitor persists a block or confirms contracts from it. It is not a second
// consensus implementation: btcd already rejected invalid PoW / scripts.
//
// Checks:
//   - parsed header hash matches the canonical hash advertised for this height
//   - header.prev matches the canonical hash of height-1
//   - merkle root of parsed txids matches the header
func validateIngestedBlock(height int64, parsed *ParsedBlock, canonicalHash, prevCanonicalHash string) error {
	if parsed == nil {
		return fmt.Errorf("parsed block is nil")
	}
	got := strings.ToLower(strings.TrimSpace(parsed.Hash))
	if got == "" {
		got = strings.ToLower(strings.TrimSpace(parsed.Header.Hash))
	}
	if got == "" {
		return fmt.Errorf("parsed block missing header hash")
	}
	if want := strings.ToLower(strings.TrimSpace(canonicalHash)); want != "" && got != want {
		return fmt.Errorf("block hash mismatch at %d: parsed=%s canonical=%s", height, got, want)
	}
	if height > 0 {
		if wantPrev := strings.ToLower(strings.TrimSpace(prevCanonicalHash)); wantPrev != "" {
			gotPrev := strings.ToLower(strings.TrimSpace(parsed.Header.PrevBlock))
			if gotPrev != wantPrev {
				return fmt.Errorf("prev-hash mismatch at %d: header=%s canonical=%s", height, gotPrev, wantPrev)
			}
		}
	}
	return verifyParsedMerkleRoot(parsed)
}

// verifyParsedMerkleRoot recomputes the Bitcoin merkle root from parsed txids.
func verifyParsedMerkleRoot(parsed *ParsedBlock) error {
	if parsed == nil {
		return fmt.Errorf("parsed block is nil")
	}
	want := strings.ToLower(strings.TrimSpace(parsed.Header.MerkleRoot))
	if want == "" {
		return fmt.Errorf("header missing merkle root")
	}
	txids := make([]string, 0, len(parsed.Transactions))
	for _, tx := range parsed.Transactions {
		id := strings.TrimSpace(tx.TxID)
		if id == "" {
			return fmt.Errorf("transaction missing txid")
		}
		txids = append(txids, id)
	}
	got, err := computeMerkleRootHex(txids)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("merkle root mismatch: computed=%s header=%s (txs=%d)", got, want, len(txids))
	}
	return nil
}

// computeMerkleRootHex returns the display-order (big-endian hex) merkle root
// for a list of display-order txids. Empty list is an error (blocks have a coinbase).
func computeMerkleRootHex(txids []string) (string, error) {
	if len(txids) == 0 {
		return "", fmt.Errorf("no transactions to merkle")
	}
	level := make([][]byte, 0, len(txids))
	for i, id := range txids {
		b, err := decodeDisplayHash(id)
		if err != nil {
			return "", fmt.Errorf("txid[%d]: %w", i, err)
		}
		level = append(level, b)
	}
	for len(level) > 1 {
		if len(level)%2 == 1 {
			last := make([]byte, len(level[len(level)-1]))
			copy(last, level[len(level)-1])
			level = append(level, last)
		}
		next := make([][]byte, 0, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			cat := make([]byte, 0, len(level[i])+len(level[i+1]))
			cat = append(cat, level[i]...)
			cat = append(cat, level[i+1]...)
			next = append(next, doubleSHA256(cat))
		}
		level = next
	}
	return hex.EncodeToString(reverseBytes(level[0])), nil
}

func decodeDisplayHash(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("want 32 bytes, got %d", len(raw))
	}
	return reverseBytes(raw), nil
}
