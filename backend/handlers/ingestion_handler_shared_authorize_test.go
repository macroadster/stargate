package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"stargate-backend/services"
)

// authorize guards three handlers, and the existing tests reach it through
// HandleHashImage only. Fixing a shared callee is right, but it leaves the other
// two callers unpinned: nothing here would fail if one of them stopped calling
// authorize. These cover the remaining two.
//
// Both use a real ingestion service on purpose. HandleIngest and
// HandleGetIngestion check service == nil *before* authorize, so a handler with
// no service answers 503 and the assertion could not tell an enforced token from
// an ignored one.
func TestSharedAuthorizeRejectsEveryHandlerWhenIngestTokenUnset(t *testing.T) {
	svc, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("ingestion service: %v", err)
	}
	// ingestKey left empty: STARGATE_INGEST_TOKEN unset.
	h := &IngestionHandler{service: svc}

	t.Run("HandleGetIngestion", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ingest-inscription/some-id", nil)
		rec := httptest.NewRecorder()

		h.HandleGetIngestion(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d: reads are open when the token is unset (%s)",
				rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	})

	t.Run("HandleIngest", func(t *testing.T) {
		body := `{"id":"i1","filename":"a.png","method":"test","image_base64":"aGk="}`
		req := httptest.NewRequest(http.MethodPost, "/api/ingest-inscription", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.HandleIngest(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d: writes are open when the token is unset (%s)",
				rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	})
}

// The counterpart: a configured token must still admit the correct one, so
// failing closed does not read as "these endpoints are simply off".
func TestSharedAuthorizeAdmitsConfiguredTokenOnGet(t *testing.T) {
	svc, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("ingestion service: %v", err)
	}
	h := &IngestionHandler{service: svc, ingestKey: "secret"}

	req := httptest.NewRequest(http.MethodGet, "/api/ingest-inscription/missing-id", nil)
	req.Header.Set("X-Ingest-Token", "secret")
	rec := httptest.NewRecorder()

	h.HandleGetIngestion(rec, req)

	// 404 because the record does not exist. The point is that it got past
	// authorization rather than being refused.
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("the configured token was refused: %s", rec.Body.String())
	}
}
