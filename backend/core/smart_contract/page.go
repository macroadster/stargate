package smart_contract

import (
	"strings"
	"time"
)

const (
	// DefaultPageLimit is the REST/MCP page size when limit is omitted.
	DefaultPageLimit = 50
	// MaxPageLimit caps a caller-supplied limit.
	MaxPageLimit = 500
)

// Page is the shared list pagination envelope for REST and MCP.
// Every list response should include these fields with the applied defaults
// (empty strings when there is no next page — not omitted).
type Page struct {
	Limit          int    `json:"limit"`
	Offset         int    `json:"offset"`
	HasMore        bool   `json:"has_more"`
	NextCursor     string `json:"next_cursor"`
	NextCursorDate string `json:"next_cursor_date"`
	Total          int    `json:"total"`
}

// PageQuery is the incoming pagination request (query string or MCP args).
type PageQuery struct {
	Limit      int
	Offset     int
	Cursor     string
	CursorDate *time.Time
	CursorType string // before (default) or after
}

// NewPageQuery normalizes limit/offset and parses cursor fields.
// cursorType is "before" unless the caller passes "after".
func NewPageQuery(limit, offset int, cursor, cursorDate, cursorType string, defaultLimit int) PageQuery {
	q := PageQuery{
		Limit:      NormalizeLimit(limit, defaultLimit),
		Offset:     NormalizeOffset(offset),
		Cursor:     strings.TrimSpace(cursor),
		CursorDate: ParseCursorDate(cursorDate),
		CursorType: ParseCursorType(cursorType),
	}
	if q.Cursor == "*" {
		q.Cursor = ""
	}
	return q
}

// NormalizeLimit applies the shared default and max. limit <= 0 means "use default".
func NormalizeLimit(limit, def int) int {
	if def <= 0 {
		def = DefaultPageLimit
	}
	if limit <= 0 {
		return def
	}
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return limit
}

// NormalizeOffset rejects negative offsets.
func NormalizeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

// HasMoreFromFetch reports whether another page may exist.
// fetchedCount is the store result length before post-filter collapse
// (twin-dedupe, manifest hide). A full pre-collapse page means more rows
// may remain even when the visible page shrinks below limit.
func HasMoreFromFetch(fetchedCount, pageLen, limit int) bool {
	if limit <= 0 {
		return false
	}
	if fetchedCount >= limit {
		return true
	}
	return pageLen >= limit
}

// FormatCursorDate formats a cursor timestamp as RFC3339 UTC.
func FormatCursorDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// ParseCursorDate accepts RFC3339, RFC3339Nano, or SQLite datetime text.
func ParseCursorDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

// ParseCursorType returns "after" or the default "before".
func ParseCursorType(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "after") {
		return "after"
	}
	return "before"
}

// OverFetchLimit is the store Limit to request so has_more can be exact
// (window longer than the caller page means another page exists).
func OverFetchLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	return limit + 1
}

// HasMoreFromWindow is true when a limit+1 fetch returned more than a page.
func HasMoreFromWindow(windowLen, limit int) bool {
	return limit > 0 && windowLen > limit
}

// TrimWindow drops the extra over-fetch row.
func TrimWindow[T any](window []T, limit int) []T {
	if limit > 0 && len(window) > limit {
		return window[:limit]
	}
	return window
}

// BuildPage constructs a response Page from a limit+1 fetch window.
// fetchedCount is the pre-trim window length (and pre-collapse when a
// post-filter ran). pageLen is what the client receives. lastID / lastDate
// should come from the last returned row.
func BuildPage(limit, offset, fetchedCount, pageLen int, lastID string, lastDate time.Time) Page {
	limit = NormalizeLimit(limit, DefaultPageLimit)
	offset = NormalizeOffset(offset)
	p := Page{
		Limit:   limit,
		Offset:  offset,
		HasMore: HasMoreFromWindow(fetchedCount, limit),
		Total:   pageLen,
	}
	if p.HasMore {
		p.NextCursor = strings.TrimSpace(lastID)
		p.NextCursorDate = FormatCursorDate(lastDate)
	}
	return p
}

// Fields returns the stable JSON pagination keys for map responses.
func (p Page) Fields() map[string]interface{} {
	return map[string]interface{}{
		"limit":            p.Limit,
		"offset":           p.Offset,
		"has_more":         p.HasMore,
		"next_cursor":      p.NextCursor,
		"next_cursor_date": p.NextCursorDate,
		"total":            p.Total,
	}
}

// ApplyToTask copies pagination onto a task list filter.
func (q PageQuery) ApplyToTask(f *TaskFilter) {
	if f == nil {
		return
	}
	f.Limit = q.Limit
	f.Offset = q.Offset
	f.CursorID = q.Cursor
	f.CursorDate = q.CursorDate
	f.CursorType = q.CursorType
}

// ApplyToSubmission copies pagination onto a submission list filter.
func (q PageQuery) ApplyToSubmission(f *SubmissionFilter) {
	if f == nil {
		return
	}
	f.Limit = q.Limit
	f.Offset = q.Offset
	f.CursorID = q.Cursor
	f.CursorDate = q.CursorDate
	f.CursorType = q.CursorType
}

// ApplyToProposal copies pagination onto a proposal list filter.
func (q PageQuery) ApplyToProposal(f *ProposalFilter) {
	if f == nil {
		return
	}
	f.Limit = q.Limit
	f.MaxResults = q.Limit
	f.Offset = q.Offset
	f.CursorID = q.Cursor
	f.CursorDate = q.CursorDate
	f.CursorType = q.CursorType
}
