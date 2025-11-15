package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type InscriptionRequest struct {
	ImageData string  `json:"imageData"`
	Text      string  `json:"text"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
	ID        string  `json:"id"`
	Status    string  `json:"status"`
}

var pendingInscriptions []InscriptionRequest

func enableCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
}

func handleBlocks(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	resp, err := http.Get("https://mempool.space/api/v1/blocks")
	if err != nil {
		http.Error(w, "Failed to fetch blocks", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// Add future block
	var blocks []interface{}
	if err := json.Unmarshal(body, &blocks); err == nil && len(blocks) > 0 {
		futureBlock := map[string]interface{}{
			"id":       "future",
			"height":   blocks[0].(map[string]interface{})["height"].(float64) + 1,
			"timestamp": time.Now().Unix() + 600,
			"hash":    "pending...",
			"tx_count": len(pendingInscriptions),
		}
		blocks = append([]interface{}{futureBlock}, blocks...)
		body, _ = json.Marshal(blocks)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func handleInscriptions(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	resp, err := http.Get("https://api.hiro.so/ordinals/v1/inscriptions?order_by=number&order=desc&limit=100")
	if err != nil {
		http.Error(w, "Failed to fetch inscriptions", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func handleInscribe(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	text := r.FormValue("text")
	priceStr := r.FormValue("price")
	price, _ := strconv.ParseFloat(priceStr, 64)

	req := InscriptionRequest{
		Text:      text,
		Price:     price,
		Timestamp: time.Now().Unix(),
		ID:        fmt.Sprintf("pending_%d", time.Now().Unix()),
		Status:    "pending",
	}

	pendingInscriptions = append(pendingInscriptions, req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "id": req.ID})
}

func handlePendingTransactions(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pendingInscriptions)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"inscriptions": pendingInscriptions,
			"blocks":       []interface{}{},
		})
		return
	}

	// Search pending inscriptions
	var results []InscriptionRequest
	for _, insc := range pendingInscriptions {
		if strings.Contains(strings.ToLower(insc.Text), strings.ToLower(query)) ||
		   strings.Contains(strings.ToLower(insc.ID), strings.ToLower(query)) {
			results = append(results, insc)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"inscriptions": results,
		"blocks":       []interface{}{},
	})
}

func handleInscriptionContent(w http.ResponseWriter, r *http.Request) {
	enableCORS(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/inscription/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "content" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	id := parts[0]

	resp, err := http.Get(fmt.Sprintf("https://api.hiro.so/ordinals/v1/inscriptions/%s/content", id))
	if err != nil {
		http.Error(w, "Failed to fetch inscription content", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	// Set content type from response
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Write(body)
}

func main() {
	http.HandleFunc("/api/blocks", handleBlocks)
	http.HandleFunc("/api/inscriptions", handleInscriptions)
	http.HandleFunc("/api/inscribe", handleInscribe)
	http.HandleFunc("/api/pending-transactions", handlePendingTransactions)
	http.HandleFunc("/api/search", handleSearch)
	http.HandleFunc("/api/inscription/", handleInscriptionContent)

	fmt.Println("Server starting on :3001")
	log.Fatal(http.ListenAndServe(":3001", nil))
}