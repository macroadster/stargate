package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"stargate-backend/services"
)

func TestHandleHashImageRejectsWhenIngestTokenUnset(t *testing.T) {
	h := &IngestionHandler{}
	req := httptest.NewRequest(http.MethodPost, "/api/ingest-hash", strings.NewReader(`{"image_base64":"aGk="}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleHashImage(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIngestionEndpointsRejectWhenIngestTokenUnset(t *testing.T) {
	svc, err := services.NewIngestionService(filepath.Join(t.TempDir(), "ingestion.db"))
	if err != nil {
		t.Fatalf("create ingestion service: %v", err)
	}
	h := &IngestionHandler{service: svc}
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "ingest",
			method: http.MethodPost,
			path:   "/api/ingest-inscription",
			body:   `{"id":"id","filename":"test.png","method":"test","image_base64":"aGk="}`,
			call:   h.HandleIngest,
		},
		{
			name:   "get ingestion",
			method: http.MethodGet,
			path:   "/api/ingest-inscription/id",
			call:   h.HandleGetIngestion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			tc.call(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestHandleHashImageRequiresConfiguredIngestToken(t *testing.T) {
	h := &IngestionHandler{ingestKey: "secret"}

	t.Run("missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ingest-hash", strings.NewReader(`{"image_base64":"aGk="}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.HandleHashImage(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("matching token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ingest-hash", strings.NewReader(`{"image_base64":"aGk="}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Ingest-Token", "secret")
		rec := httptest.NewRecorder()

		h.HandleHashImage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})
}
