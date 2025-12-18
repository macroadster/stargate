package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"stargate-backend/services"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
)

type IngestionHandler struct {
	service   *services.IngestionService
	ingestKey string
}

type ingestRequest struct {
	ID            string                 `json:"id"`
	Filename      string                 `json:"filename"`
	Method        string                 `json:"method"`
	MessageLength int                    `json:"message_length"`
	ImageBase64   string                 `json:"image_base64"`
	Metadata      map[string]interface{} `json:"metadata"`
}

func NewIngestionHandler(service *services.IngestionService) *IngestionHandler {
	return &IngestionHandler{
		service:   service,
		ingestKey: os.Getenv("STARGATE_INGEST_TOKEN"),
	}
}

func (h *IngestionHandler) authorize(r *http.Request) bool {
	if h.ingestKey == "" {
		return true
	}
	token := r.Header.Get("X-Ingest-Token")
	return token != "" && token == h.ingestKey
}

func (h *IngestionHandler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.service == nil {
		http.Error(w, "ingestion service not configured", http.StatusServiceUnavailable)
		return
	}
	if !h.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req ingestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Filename == "" || req.Method == "" || req.ImageBase64 == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}
	if _, err := base64.StdEncoding.DecodeString(req.ImageBase64); err != nil {
		http.Error(w, "invalid image_base64", http.StatusBadRequest)
		return
	}

	rec := services.IngestionRecord{
		ID:            req.ID,
		Filename:      req.Filename,
		Method:        req.Method,
		MessageLength: req.MessageLength,
		ImageBase64:   req.ImageBase64,
		Metadata:      req.Metadata,
		Status:        "pending",
	}

	if err := h.service.Create(rec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     rec.ID,
		"status": rec.Status,
	})
}

func (h *IngestionHandler) HandleGetIngestion(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.service == nil {
		http.Error(w, "ingestion service not configured", http.StatusServiceUnavailable)
		return
	}
	if !h.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// path expected: /api/ingest-inscription/{id}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/ingest-inscription/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	id := parts[0]
	rec, err := h.service.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rec)
}

// HandleHashImage returns hash metadata for an uploaded image without storing it.
func (h *IngestionHandler) HandleHashImage(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	imageData, err := readImagePayload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sha := sha256.Sum256(imageData)
	hash160 := btcutil.Hash160(imageData)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sha256":        hex.EncodeToString(sha[:]),
		"hash160":       hex.EncodeToString(hash160),
		"byte_length":   len(imageData),
		"pixel_hash_32": hex.EncodeToString(sha[:]),
		"pixel_hash_20": hex.EncodeToString(hash160),
	})
}

func readImagePayload(r *http.Request) ([]byte, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var body struct {
			ImageBase64 string `json:"image_base64"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		if body.ImageBase64 == "" {
			return nil, errors.New("image_base64 is required")
		}
		return base64.StdEncoding.DecodeString(body.ImageBase64)
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}

	if formValue := r.FormValue("image_base64"); formValue != "" {
		return base64.StdEncoding.DecodeString(formValue)
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

// enableCORS matches other handlers.
func enableCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Ingest-Token")
}
