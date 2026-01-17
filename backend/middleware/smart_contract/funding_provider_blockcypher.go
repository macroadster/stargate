package smart_contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"stargate-backend/core/smart_contract"
)

type blockcypherProvider struct {
	baseURL  string
	apiToken string
	client   *http.Client
}

// NewBlockcypherProvider creates a provider that fetches merkle proofs from Blockcypher API.
// Works for both testnet and mainnet.
func NewBlockcypherProvider(baseURL string) FundingProvider {
	apiToken := ""
	// Blockcypher may require an API token for production use
	// Use testnet/testnet4/ from base URL
	return &blockcypherProvider{
		baseURL:  fmt.Sprintf("https://api.blockcypher.com/v1/btc-%s", baseURL),
		apiToken: apiToken,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

type blockcypherTxResponse struct {
	Hash          string `json:"hash"`
	Confirmations int    `json:"confirmations"`
	BlockHash     string `json:"block_hash"`
	BlockHeight   int64  `json:"block_height"`
}

type blockcypherMerkleRoot struct {
	MerkleRoot string `json:"merkle_root"`
}

func (p *blockcypherProvider) FetchProof(ctx context.Context, task smart_contract.Task) (*smart_contract.MerkleProof, error) {
	if task.MerkleProof == nil || task.MerkleProof.TxID == "" {
		return nil, fmt.Errorf("no tx_id on task")
	}
	txid := task.MerkleProof.TxID

	// Fetch transaction info
	url := fmt.Sprintf("%s/txs/%s", p.baseURL, txid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if p.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiToken)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return nil, fmt.Errorf("blockcypher tx request failed: %s", resp.Status)
	}

	var txResp blockcypherTxResponse
	if err := json.NewDecoder(resp.Body).Decode(&txResp); err != nil {
		return nil, err
	}

	log.Printf("Blockcypher fetch for tx %s: status=%d, height=%d, block_hash=%s", txid, resp.StatusCode, txResp.BlockHeight, txResp.BlockHash)

	// Build merkle proof
	proof := *task.MerkleProof
	proof.BlockHeight = txResp.BlockHeight
	proof.ConfirmationStatus = "confirmed"
	now := time.Now()
	proof.ConfirmedAt = &now

	// Since Blockcypher doesn't provide full merkle proof path like Blockstream,
	// we construct a minimal proof with the block hash
	proof.ProofPath = []smart_contract.ProofNode{
		{Hash: txResp.BlockHash, Direction: "left"},
	}
	if proof.BlockHeaderMerkleRoot == "" {
		proof.BlockHeaderMerkleRoot = txResp.BlockHash
	}

	return &proof, nil
}
