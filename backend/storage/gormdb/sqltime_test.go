package gormdb

import (
	"testing"
	"time"
)

func TestSQLTimeScanFormats(t *testing.T) {
	cases := []string{
		"2026-03-09T20:49:02Z",
		"2026-07-26 05:27:20.159703951 +0000 UTC",
		"2026-07-26T05:36:52.378694+00:00",
		"2026-07-26 05:38:17",
		time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, s := range cases {
		var st SQLTime
		if err := st.Scan(s); err != nil {
			t.Errorf("Scan %q: %v", s, err)
			continue
		}
		if st.IsZero() {
			t.Errorf("Scan %q produced zero time", s)
		}
	}
	var st SQLTime
	if err := st.Scan(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if st.Year() != 2026 {
		t.Fatalf("got %v", st.Time)
	}
	v, err := st.Value()
	if err != nil || v == nil {
		t.Fatalf("Value: %v %v", v, err)
	}

	var garbage SQLTime
	if err := garbage.Scan("not-a-timestamp"); err != nil {
		t.Fatalf("unparseable timestamp must not fail scan: %v", err)
	}
	if !garbage.IsZero() {
		t.Fatalf("unparseable timestamp should be zero, got %v", garbage.Time)
	}
	var unix SQLTime
	if err := unix.Scan("1710000000"); err != nil || unix.IsZero() {
		t.Fatalf("unix seconds: err=%v t=%v", err, unix.Time)
	}
}
