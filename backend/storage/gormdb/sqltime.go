package gormdb

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

// SQLTime is a time.Time that scans both Postgres timestamptz (time.Time) and
// SQLite TEXT/DATETIME columns (string). Production DBs created before the GORM
// migration often declare created_at as TEXT; modernc returns those as string
// and database/sql cannot assign string into *time.Time.
//
// Use this for any GORM model field that may live in a legacy TEXT timestamp
// column. Fresh AutoMigrate DATETIME columns also work.
type SQLTime struct {
	time.Time
}

// NewSQLTime wraps t.
func NewSQLTime(t time.Time) SQLTime {
	return SQLTime{Time: t}
}

// Scan implements sql.Scanner.
func (t *SQLTime) Scan(value interface{}) error {
	if t == nil {
		return fmt.Errorf("gormdb.SQLTime: Scan on nil receiver")
	}
	if value == nil {
		t.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t.Time = v
		return nil
	case string:
		return t.parse(v)
	case []byte:
		return t.parse(string(v))
	case int64:
		t.Time = time.Unix(v, 0).UTC()
		return nil
	case float64:
		t.Time = time.Unix(int64(v), 0).UTC()
		return nil
	default:
		return fmt.Errorf("gormdb.SQLTime: cannot scan type %T", value)
	}
}

func (t *SQLTime) parse(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		// Go's time.Time.String() — written by some older code paths
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700 UTC",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999+00:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		// SQLite datetime('now')
		"2006-01-02 15:04:05.000",
	}
	for _, f := range formats {
		parsed, err := time.Parse(f, s)
		if err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	// Unix seconds / millis written as TEXT by older stores.
	if unix, err := parseUnixLike(s); err == nil {
		t.Time = unix
		return nil
	}
	// Unparseable timestamps must not fail the whole row scan — login used to
	// 500 when Validate (COUNT) succeeded but Get/First could not scan created_at.
	t.Time = time.Time{}
	return nil
}

func parseUnixLike(s string) (time.Time, error) {
	var n int64
	if _, err := fmt.Sscan(s, &n); err != nil {
		return time.Time{}, err
	}
	if n <= 0 {
		return time.Time{}, fmt.Errorf("non-positive unix")
	}
	// millis if it looks like 13+ digits
	if n > 1_000_000_000_000 {
		return time.UnixMilli(n).UTC(), nil
	}
	return time.Unix(n, 0).UTC(), nil
}

// Value implements driver.Valuer. Always emit RFC3339Nano UTC for portability.
func (t SQLTime) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time.UTC().Format(time.RFC3339Nano), nil
}

// ToTime returns the underlying time.Time.
func (t SQLTime) ToTime() time.Time { return t.Time }
