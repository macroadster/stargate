package smart_contract

import (
	"testing"
	"time"
)

func TestNormalizeLimit(t *testing.T) {
	if got := NormalizeLimit(0, 0); got != DefaultPageLimit {
		t.Fatalf("default = %d", got)
	}
	if got := NormalizeLimit(10, DefaultPageLimit); got != 10 {
		t.Fatalf("explicit = %d", got)
	}
	if got := NormalizeLimit(MaxPageLimit+1, DefaultPageLimit); got != MaxPageLimit {
		t.Fatalf("capped = %d", got)
	}
	if got := NormalizeOffset(-3); got != 0 {
		t.Fatalf("offset = %d", got)
	}
}

func TestHasMoreFromFetchMatchesContractTwinDedupe(t *testing.T) {
	// SQL returned a full page of 12; twin dedupe left 11 visible.
	if !HasMoreFromFetch(12, 11, 12) {
		t.Fatal("full pre-dedupe page must report has_more")
	}
	if HasMoreFromFetch(11, 11, 12) {
		t.Fatal("short pre-dedupe page must not report has_more")
	}
	if !HasMoreFromFetch(0, 12, 12) {
		t.Fatal("pageLen alone still signals full page when fetch count unknown")
	}
	if HasMoreFromFetch(0, 0, 12) {
		t.Fatal("empty page has no more")
	}
}

func TestBuildPageDefaultsAndCursor(t *testing.T) {
	ts := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	p := BuildPage(0, -1, 51, 50, "id-50", ts)
	if p.Limit != DefaultPageLimit || p.Offset != 0 {
		t.Fatalf("defaults %+v", p)
	}
	if !p.HasMore || p.NextCursor != "id-50" || p.NextCursorDate != "2026-08-13T12:00:00Z" {
		t.Fatalf("cursor %+v", p)
	}
	if p.Total != 50 {
		t.Fatalf("total %d", p.Total)
	}

	short := BuildPage(50, 0, 3, 3, "id-3", ts)
	if short.HasMore || short.NextCursor != "" || short.NextCursorDate != "" {
		t.Fatalf("short page must clear cursors: %+v", short)
	}
	exact := BuildPage(1, 0, 1, 1, "only", ts)
	if exact.HasMore {
		t.Fatal("exact last page from limit+1 fetch must not claim has_more")
	}
}

func TestParseCursorDateAndType(t *testing.T) {
	if ParseCursorDate("") != nil {
		t.Fatal("empty")
	}
	rfc := ParseCursorDate("2026-07-10T12:00:00Z")
	if rfc == nil || rfc.UTC().Format(time.RFC3339) != "2026-07-10T12:00:00Z" {
		t.Fatalf("rfc %v", rfc)
	}
	space := ParseCursorDate("2026-07-10 12:00:00")
	if space == nil || !space.Equal(*rfc) {
		t.Fatalf("sqlite space %v", space)
	}
	if ParseCursorType("AFTER") != "after" {
		t.Fatal("after")
	}
	if ParseCursorType("") != "before" {
		t.Fatal("default before")
	}
}

func TestNewPageQueryStarCursor(t *testing.T) {
	q := NewPageQuery(0, 0, "*", "2026-07-10T12:00:00Z", "", 0)
	if q.Limit != DefaultPageLimit || q.Cursor != "" || q.CursorDate == nil || q.CursorType != "before" {
		t.Fatalf("%+v", q)
	}
}
