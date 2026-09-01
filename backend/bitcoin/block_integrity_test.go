package bitcoin

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestComputeMerkleRootSingleTxIsTxid(t *testing.T) {
	txid := "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"
	got, err := computeMerkleRootHex([]string{txid})
	if err != nil {
		t.Fatal(err)
	}
	if got != txid {
		t.Fatalf("single-tx merkle must equal txid: got %s", got)
	}
}

func TestComputeMerkleRootTwoTxs(t *testing.T) {
	a := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	got, err := computeMerkleRootHex([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	// Bitcoin merkle: hash(le(a) || le(b)), then display-reverse.
	ae, _ := hex.DecodeString(a)
	be, _ := hex.DecodeString(b)
	cat := append(reverseBytes(ae), reverseBytes(be)...)
	h1 := sha256.Sum256(cat)
	h2 := sha256.Sum256(h1[:])
	want := hex.EncodeToString(reverseBytes(h2[:]))
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestValidateIngestedBlockRejectsHashAndPrevMismatch(t *testing.T) {
	txid := "1111111111111111111111111111111111111111111111111111111111111111"
	root, err := computeMerkleRootHex([]string{txid})
	if err != nil {
		t.Fatal(err)
	}
	parsed := &ParsedBlock{
		Height: 10,
		Hash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Header: BlockHeader{
			Hash:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			PrevBlock:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			MerkleRoot: root,
		},
		Transactions: []Transaction{{TxID: txid}},
	}
	if err := validateIngestedBlock(10, parsed, parsed.Hash, parsed.Header.PrevBlock); err != nil {
		t.Fatalf("valid block rejected: %v", err)
	}
	if err := validateIngestedBlock(10, parsed, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", parsed.Header.PrevBlock); err == nil {
		t.Fatal("expected canonical hash mismatch")
	}
	if err := validateIngestedBlock(10, parsed, parsed.Hash, "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"); err == nil {
		t.Fatal("expected prev-hash mismatch")
	}
	parsed.Header.MerkleRoot = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if err := validateIngestedBlock(10, parsed, parsed.Hash, parsed.Header.PrevBlock); err == nil {
		t.Fatal("expected merkle mismatch")
	}
}

func TestComputeMerkleRootEmptyRejected(t *testing.T) {
	if _, err := computeMerkleRootHex(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateIngestedBlockSkipsMerkleWhenParseIncomplete(t *testing.T) {
	parsed := &ParsedBlock{
		Height: 10,
		Hash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Header: BlockHeader{
			Hash:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			PrevBlock:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			MerkleRoot: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
		Transactions:      []Transaction{{TxID: "1111111111111111111111111111111111111111111111111111111111111111"}},
		AdvertisedTxCount: 2,
		ParseIncomplete:   true,
	}
	if err := validateIngestedBlock(10, parsed, parsed.Hash, parsed.Header.PrevBlock); err != nil {
		t.Fatalf("incomplete parse must not fail-close on merkle: %v", err)
	}
	if err := validateIngestedBlock(10, parsed, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", parsed.Header.PrevBlock); err == nil {
		t.Fatal("header hash mismatch must still reject")
	}
}
