package handlers

import (
	"net/http"

	_ "image/gif"
	_ "image/jpeg"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"stargate-backend/services"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	*BaseHandler
	healthService *services.HealthService
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(healthService *services.HealthService) *HealthHandler {
	return &HealthHandler{
		BaseHandler:   NewBaseHandler(),
		healthService: healthService,
	}
}

// HandleHealth handles health check requests
func (h *HealthHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	health := h.healthService.GetHealthStatus()
	h.sendSuccess(w, health)
}

// DiscoveryHandler handles peer discovery requests for WebRTC
type DiscoveryHandler struct {
	*BaseHandler
	peerService *services.PeerService
}

// NewDiscoveryHandler creates a new discovery handler
func NewDiscoveryHandler(peerService *services.PeerService) *DiscoveryHandler {
	return &DiscoveryHandler{
		BaseHandler: NewBaseHandler(),
		peerService: peerService,
	}
}

// BlockHandler handles block-related requests
type BlockHandler struct {
	*BaseHandler
	blockService *services.BlockService
}

// NewBlockHandler creates a new block handler
func NewBlockHandler(blockService *services.BlockService) *BlockHandler {
	return &BlockHandler{
		BaseHandler:  NewBaseHandler(),
		blockService: blockService,
	}
}

// HandleRegisterPeer registers a new peer ID
func (h *DiscoveryHandler) HandleRegisterPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		PeerID string `json:"peerId"`
	}
	if err := h.parseJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.PeerID == "" {
		h.sendError(w, http.StatusBadRequest, "peerId is required")
		return
	}

	h.peerService.Register(req.PeerID)
	h.sendSuccess(w, map[string]string{"status": "registered"})
}

// HandleUnregisterPeer unregisters a peer ID
func (h *DiscoveryHandler) HandleUnregisterPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		PeerID string `json:"peerId"`
	}
	if err := h.parseJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.PeerID == "" {
		h.sendError(w, http.StatusBadRequest, "peerId is required")
		return
	}

	h.peerService.Unregister(req.PeerID)
	h.sendSuccess(w, map[string]string{"status": "unregistered"})
}

// HandleListPeers returns a list of active peer IDs
func (h *DiscoveryHandler) HandleListPeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	peers := h.peerService.GetPeers()
	h.sendSuccess(w, peers)
}

// HandleGetBlocks handles getting blocks
func (h *BlockHandler) HandleGetBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	blocks, err := h.blockService.GetBlocks()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to fetch blocks")
		return
	}

	h.sendSuccess(w, blocks)
}
