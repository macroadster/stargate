package smart_contract

import (
	"stargate-backend/core/smart_contract"
)

// syncAnnouncement is a generic container for cross-instance state synchronization.
type syncAnnouncement struct {
	Type         string                       `json:"type"`
	Issuer       string                       `json:"issuer"`
	Timestamp    int64                        `json:"timestamp"`
	Event        *smart_contract.Event        `json:"event,omitempty"`
	Task         *smart_contract.Task         `json:"task,omitempty"`
	Claim        *smart_contract.Claim        `json:"claim,omitempty"`
	Submission   *smart_contract.Submission   `json:"submission,omitempty"`
	Proposal     *smart_contract.Proposal     `json:"proposal,omitempty"`
	Contract     *smart_contract.Contract     `json:"contract,omitempty"`
	EscortStatus *smart_contract.EscortStatus `json:"escort_status,omitempty"`
}
