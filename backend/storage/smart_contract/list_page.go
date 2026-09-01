package smart_contract

import (
	"strings"
	"time"

	core "stargate-backend/core/smart_contract"
)

func applyOffsetLimit[T any](items []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset > len(items) {
		return []T{}
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		return items[:limit]
	}
	return items
}

func matchDateCursor(t time.Time, cursor *time.Time, cursorType string) bool {
	if cursor == nil {
		return true
	}
	if t.IsZero() {
		return false
	}
	if strings.EqualFold(cursorType, "after") {
		return t.After(*cursor)
	}
	return t.Before(*cursor)
}

// matchDateIDCursor is keyset (date, id) so equal timestamps do not skip or duplicate.
func matchDateIDCursor(t time.Time, id string, cursorDate *time.Time, cursorID, cursorType string) bool {
	if cursorDate == nil && strings.TrimSpace(cursorID) == "" {
		return true
	}
	after := strings.EqualFold(cursorType, "after")
	if cursorDate != nil && !t.IsZero() {
		if after {
			if t.After(*cursorDate) {
				return true
			}
			if t.Before(*cursorDate) {
				return false
			}
		} else {
			if t.Before(*cursorDate) {
				return true
			}
			if t.After(*cursorDate) {
				return false
			}
		}
	} else if cursorDate != nil && t.IsZero() {
		return false
	}
	if cursorID == "" {
		// Date-only cursor (contracts-style): equal timestamps are excluded.
		return false
	}
	if after {
		return id > cursorID
	}
	return id < cursorID
}

func matchIDCursor(id, cursorID, cursorType string) bool {
	if strings.TrimSpace(cursorID) == "" {
		return true
	}
	if strings.EqualFold(cursorType, "after") {
		return id > cursorID
	}
	return id < cursorID
}

func applySubmissionPage(subs []core.Submission, filter core.SubmissionFilter) []core.Submission {
	out := make([]core.Submission, 0, len(subs))
	for _, sub := range subs {
		if !matchDateIDCursor(sub.CreatedAt, sub.SubmissionID, filter.CursorDate, filter.CursorID, filter.CursorType) {
			continue
		}
		out = append(out, sub)
	}
	return applyOffsetLimit(out, filter.Offset, filter.Limit)
}
