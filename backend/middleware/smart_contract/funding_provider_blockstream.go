package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"stargate-backend/core/smart_contract"
)

type blockstreamProvider struct {
	baseURL string
	client  *http.Client
}

// NewBlockstreamFundingProvider builds a provider that fetches merkle proofs from a Blockstream-compatible API.
// Only works with blockstream.info or blockcypher.com APIs.
func NewBlockstreamFundingProvider(baseURL string) FundingProvider {
	if baseURL == "" {
		// Use mainnet as default
		baseURL = "https://blockstream.info/api"
	}
	return &blockstreamProvider{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type merkleProofResponse struct {
	BlockHeight int      `json:"block_height"`
	Merkle      []string `json:"merkle"`
	Pos         int      `json:"pos"`
}

func (p *blockstreamProvider) FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error) {
	if task.MerkleProof == nil || task.MerkleProof.TxID == "" {
		return nil, fmt.Errorf("no tx_id on task")
	}
	txid := task.MerkleProof.TxID

	url := fmt.Sprintf("%s/tx/%s/merkle-proof", p.baseURL, txid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("merkle proof request failed: %s", resp.Status)
	}
	var mp merkleProofResponse
	if err := json.NewDecoder(resp.Body).Decode(&mp); err != nil {
		return nil, err
	}

	proof := *task.MerkleProof
	proof.BlockHeight = int64(mp.BlockHeight)
	proof.ConfirmationStatus = "confirmed"
	now := time.Now()
	proof.ConfirmedAt = &now
	proof.ProofPath = make([]smart_contract.ProofNode, 0, len(mp.Merkle))
	for _, h := range mp.Merkle {
		proof.ProofPath = append(proof.ProofPath, smart_contract.ProofNode{Hash: h, Direction: "left"})
	}
	if proof.BlockHeaderMerkleRoot == "" && len(mp.Merkle) > 0 {
		proof.BlockHeaderMerkleRoot = mp.Merkle[len(mp.Merkle)-1]
	}
	return &proof, nil
}
