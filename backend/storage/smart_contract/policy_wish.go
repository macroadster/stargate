package smart_contract

import (
	"fmt"
	"strings"
	"time"

	"stargate-backend/core/identity"
	"stargate-backend/core/smart_contract"
)

// DeleteWishPlan is the dialect-agnostic cascade target for deleting a wish.
type DeleteWishPlan struct {
	VisiblePixelHash string
	WishID           string // wish-<hash>
}

// BuildDeleteWishPlan normalizes visible pixel hash → wish contract id.
func BuildDeleteWishPlan(visiblePixelHash string) (DeleteWishPlan, error) {
	v := strings.TrimSpace(visiblePixelHash)
	if v == "" {
		return DeleteWishPlan{}, fmt.Errorf("visible_pixel_hash required")
	}
	return DeleteWishPlan{
		VisiblePixelHash: v,
		WishID:           identity.ToWishID(v),
	}, nil
}

// BuildReworkRequest constructs a new open rework request (shared id/status shape).
func BuildReworkRequest(contractID, requester, notes string, now time.Time, requestID string) (smart_contract.ContractReworkRequest, error) {
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return smart_contract.ContractReworkRequest{}, fmt.Errorf("contract_id required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(requestID) == "" {
		requestID = fmt.Sprintf("rework-%s-%d", contractID, now.UnixNano())
	}
	return smart_contract.ContractReworkRequest{
		RequestID:  requestID,
		ContractID: contractID,
		Requester:  strings.TrimSpace(requester),
		Notes:      notes,
		Status:     smart_contract.ReworkStatusOpen,
		CreatedAt:  now,
	}, nil
}

// ReworkTaskStatusOnCreate is the task status applied when a rework request is opened.
func ReworkTaskStatusOnCreate() string { return "rejected" }

// AppendReworkRequestToMetadata adds a rework request into contract metadata JSON map.
func AppendReworkRequestToMetadata(meta map[string]interface{}, req smart_contract.ContractReworkRequest) map[string]interface{} {
	if meta == nil {
		meta = map[string]interface{}{}
	}
	reworkReqs := []interface{}{}
	if existing, ok := meta["rework_requests"].([]interface{}); ok {
		reworkReqs = existing
	}
	entry := map[string]interface{}{
		"request_id":  req.RequestID,
		"contract_id": req.ContractID,
		"requester":   req.Requester,
		"notes":       req.Notes,
		"status":      req.Status,
		"created_at":  req.CreatedAt.Format(time.RFC3339),
	}
	reworkReqs = append(reworkReqs, entry)
	meta["rework_requests"] = reworkReqs
	return meta
}

// ResolveReworkRequestInMetadata marks a rework request resolved in metadata.
// Returns updated meta or error if not found.
func ResolveReworkRequestInMetadata(meta map[string]interface{}, requestID string, now time.Time) (map[string]interface{}, error) {
	if meta == nil {
		meta = map[string]interface{}{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	requestID = strings.TrimSpace(requestID)
	reworkReqs, ok := meta["rework_requests"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("rework request %s not found", requestID)
	}
	found := false
	resolvedAt := now.Format(time.RFC3339)
	for i, r := range reworkReqs {
		rMap, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if rid, ok := rMap["request_id"].(string); ok && rid == requestID {
			rMap["status"] = smart_contract.ReworkStatusResolved
			rMap["resolved_at"] = resolvedAt
			reworkReqs[i] = rMap
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("rework request %s not found", requestID)
	}
	meta["rework_requests"] = reworkReqs
	return meta, nil
}

// ParseReworkRequestsFromMetadata extracts rework requests from contract metadata.
func ParseReworkRequestsFromMetadata(meta map[string]interface{}) []smart_contract.ContractReworkRequest {
	if meta == nil {
		return nil
	}
	raw, ok := meta["rework_requests"].([]interface{})
	if !ok {
		return nil
	}
	var reqs []smart_contract.ContractReworkRequest
	for _, r := range raw {
		rMap, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		req := smart_contract.ContractReworkRequest{}
		if id, ok := rMap["request_id"].(string); ok {
			req.RequestID = id
		}
		if cid, ok := rMap["contract_id"].(string); ok {
			req.ContractID = cid
		}
		if requester, ok := rMap["requester"].(string); ok {
			req.Requester = requester
		}
		if notes, ok := rMap["notes"].(string); ok {
			req.Notes = notes
		}
		if status, ok := rMap["status"].(string); ok {
			req.Status = status
		}
		if createdAt, ok := rMap["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
				req.CreatedAt = t
			} else if t, err := parseSQLiteTime(createdAt); err == nil && t != nil {
				req.CreatedAt = *t
			}
		}
		if resolvedAt, ok := rMap["resolved_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, resolvedAt); err == nil {
				req.ResolvedAt = &t
			} else if t, err := parseSQLiteTime(resolvedAt); err == nil {
				req.ResolvedAt = t
			}
		}
		reqs = append(reqs, req)
	}
	return reqs
}
