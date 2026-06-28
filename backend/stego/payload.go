package stego

// Payload captures the proposal/task data embedded in a stego image.
//
// Schema v2 embeds the full payload (including manifest fields) as JSON
// directly in the alpha channel — no separate IPFS file needed.
// Schema v1 (legacy) only embedded a YAML manifest pointer; the payload
// lived in a separate IPFS object referenced by PayloadCID.
type Payload struct {
	SchemaVersion int             `json:"schema_version"`
	Proposal      PayloadProposal `json:"proposal"`
	Tasks         []PayloadTask   `json:"tasks,omitempty"`
	Metadata      []MetadataEntry `json:"metadata,omitempty"`

	// Manifest fields (v2 inline — previously in separate YAML manifest)
	ProposalID       string `json:"proposal_id,omitempty"`
	VisiblePixelHash string `json:"visible_pixel_hash,omitempty"`
	Issuer           string `json:"issuer,omitempty"`
	CreatedAt        int64  `json:"created_at,omitempty"`
	SandboxHash      string `json:"sandbox_hash,omitempty"`
	ContractID       string `json:"contract_id,omitempty"`
}

type PayloadProposal struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	DescriptionMD    string `json:"description_md"`
	BudgetSats       int64  `json:"budget_sats"`
	VisiblePixelHash string `json:"visible_pixel_hash"`
	CreatedAt        int64  `json:"created_at"`
}

type PayloadTask struct {
	TaskID           string   `json:"task_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	BudgetSats       int64    `json:"budget_sats"`
	Skills           []string `json:"skills_required,omitempty"`
	Status           string   `json:"status,omitempty"`
	ContractorWallet string   `json:"contractor_wallet,omitempty"`
}

type MetadataEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
