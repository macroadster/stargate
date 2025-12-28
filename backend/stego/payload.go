package stego

// Payload captures the off-chain proposal/task data referenced by a stego manifest.
type Payload struct {
	SchemaVersion int             `json:"schema_version"`
	Proposal      PayloadProposal `json:"proposal"`
	Tasks         []PayloadTask   `json:"tasks,omitempty"`
	Metadata      []MetadataEntry `json:"metadata,omitempty"`
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
	ContractorWallet string   `json:"contractor_wallet,omitempty"`
}

type MetadataEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
