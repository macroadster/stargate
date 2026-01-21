package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"stargate-backend/models"
)

// InscriptionService handles inscription-related business logic
type InscriptionService struct {
	inscriptionsFile string
	mu               sync.RWMutex
}

// NewInscriptionService creates a new inscription service
func NewInscriptionService(inscriptionsFile string) *InscriptionService {
	return &InscriptionService{
		inscriptionsFile: inscriptionsFile,
	}
}

// GetAllInscriptions retrieves all pending inscriptions
func (s *InscriptionService) GetAllInscriptions() ([]models.InscriptionRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadInscriptions()
}

// loadInscriptions loads inscriptions from file without locking
func (s *InscriptionService) loadInscriptions() ([]models.InscriptionRequest, error) {
	var inscriptions []models.InscriptionRequest

	file, err := os.Open(s.inscriptionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.InscriptionRequest{}, nil
		}
		return nil, fmt.Errorf("failed to open inscriptions file: %w", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&inscriptions); err != nil {
		return []models.InscriptionRequest{}, fmt.Errorf("failed to decode inscriptions: %w", err)
	}

	return inscriptions, nil
}

// CreateInscription creates a new inscription
func (s *InscriptionService) CreateInscription(req models.InscribeRequest, file io.Reader, filename string) (*models.InscriptionRequest, error) {
	// Load existing inscriptions without holding the write lock
	inscriptions, err := s.loadInscriptions()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse price
	price, _ := strconv.ParseFloat(req.Price, 64)

	// Create uploads directory if it doesn't exist
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/data/uploads"
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create uploads directory: %w", err)
	}

	// Generate filename and handle file
	timestamp := time.Now().Unix()
	var imagePath string

	// Generate filename and handle file
	if file != nil && filename != "" {
		imageFilename := fmt.Sprintf("%d_%s", timestamp, filename)
		imagePath = filepath.Join(uploadsDir, imageFilename)

		// Save file
		dst, err := os.Create(imagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to save file: %w", err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, file); err != nil {
			return nil, fmt.Errorf("failed to copy file: %w", err)
		}
	} else {
		imagePath = "" // No image provided
	}

	// Create inscription
	inscription := &models.InscriptionRequest{
		ImageData: imagePath,
		Text:      req.Text,
		Price:     price,
		Address:   req.Address,
		Timestamp: timestamp,
		ID:        fmt.Sprintf("pending_%d", timestamp),
		Status:    "pending",
	}

	// Add to inscriptions
	inscriptions = append(inscriptions, *inscription)

	// Save to file
	if err := s.saveInscriptions(inscriptions); err != nil {
		return nil, err
	}

	log.Printf("Created inscription: %s, image: %s", inscription.ID, imagePath)
	return inscription, nil
}

// SearchInscriptions searches inscriptions by query
func (s *InscriptionService) SearchInscriptions(query string) ([]models.InscriptionRequest, error) {
	inscriptions, err := s.GetAllInscriptions()
	if err != nil {
		return nil, err
	}

	var results []models.InscriptionRequest
	lowerQuery := strings.ToLower(query)

	for _, insc := range inscriptions {
		if strings.Contains(strings.ToLower(insc.Text), lowerQuery) ||
			strings.Contains(strings.ToLower(insc.ID), lowerQuery) {
			results = append(results, insc)
		}
	}

	return results, nil
}

// saveInscriptions saves inscriptions to file
func (s *InscriptionService) saveInscriptions(inscriptions []models.InscriptionRequest) error {
	file, err := os.Create(s.inscriptionsFile)
	if err != nil {
		return fmt.Errorf("failed to create inscriptions file: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(inscriptions); err != nil {
		return fmt.Errorf("failed to encode inscriptions: %w", err)
	}

	return nil
}

// BlockService handles block-related business logic
type BlockService struct {
	client *http.Client
}

// NewBlockService creates a new block service
func NewBlockService() *BlockService {
	return &BlockService{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetBlocks retrieves recent blocks from mempool.space
func (s *BlockService) GetBlocks() ([]interface{}, error) {
	resp, err := s.client.Get("https://mempool.space/api/v1/blocks")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blocks: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var blocks []interface{}
	if err := json.Unmarshal(body, &blocks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal blocks: %w", err)
	}

	return blocks, nil
}

// SearchBlocks searches blocks by query
func (s *BlockService) SearchBlocks(query string) ([]interface{}, error) {
	blocks, err := s.GetBlocks()
	if err != nil {
		return nil, err
	}

	var results []interface{}
	for _, block := range blocks {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		heightStr := fmt.Sprintf("%v", blockMap["height"])
		hash := fmt.Sprintf("%v", blockMap["id"])

		if strings.Contains(heightStr, query) || strings.Contains(strings.ToLower(hash), strings.ToLower(query)) {
			results = append(results, block)
		}
	}

	return results, nil
}

// SmartContractService handles smart contract-related business logic
type SmartContractService struct {
	contractsFile string
	mu            sync.RWMutex
}

// NewSmartContractService creates a new smart contract service
func NewSmartContractService(contractsFile string) *SmartContractService {
	return &SmartContractService{
		contractsFile: contractsFile,
	}
}

// loadContracts loads contracts from file without locking
func (s *SmartContractService) loadContracts() ([]models.SmartContractImage, error) {
	var contracts []models.SmartContractImage

	file, err := os.Open(s.contractsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.SmartContractImage{}, nil
		}
		return nil, fmt.Errorf("failed to open contracts file: %w", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&contracts); err != nil {
		return []models.SmartContractImage{}, fmt.Errorf("failed to decode contracts: %w", err)
	}

	return contracts, nil
}

// GetAllContracts retrieves all smart contracts
func (s *SmartContractService) GetAllContracts() ([]models.SmartContractImage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadContracts()
}

// CreateContract creates a new smart contract
func (s *SmartContractService) CreateContract(req models.CreateContractRequest) (*models.SmartContractImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load existing contracts
	contracts, err := s.loadContracts()
	if err != nil {
		return nil, err
	}

	// Generate stego image filename
	stegoFilename := fmt.Sprintf("stego_%s.png", req.ContractID)
	stegoURL := fmt.Sprintf("/uploads/stego-images/%s", stegoFilename)

	// Create contract
	contract := &models.SmartContractImage{
		ContractID:   req.ContractID,
		BlockHeight:  req.BlockHeight,
		StegoImage:   stegoURL,
		ContractType: req.ContractType,
		Metadata:     req.Metadata,
	}

	// Add to contracts
	contracts = append(contracts, *contract)

	// Save to file
	if err := s.saveContracts(contracts); err != nil {
		return nil, err
	}

	log.Printf("Created contract: %s with stego URL: %s", contract.ContractID, stegoURL)
	return contract, nil
}

// GetContractByID retrieves a contract by ID
func (s *SmartContractService) GetContractByID(contractID string) (*models.SmartContractImage, error) {
	contracts, err := s.GetAllContracts()
	if err != nil {
		return nil, err
	}

	for _, contract := range contracts {
		if contract.ContractID == contractID {
			return &contract, nil
		}
	}

	return nil, fmt.Errorf("contract not found: %s", contractID)
}

// saveContracts saves contracts to file
func (s *SmartContractService) saveContracts(contracts []models.SmartContractImage) error {
	if err := os.MkdirAll(filepath.Dir(s.contractsFile), 0755); err != nil {
		return fmt.Errorf("failed to create contracts directory: %w", err)
	}
	file, err := os.Create(s.contractsFile)
	if err != nil {
		return fmt.Errorf("failed to create contracts file: %w", err)
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(contracts); err != nil {
		return fmt.Errorf("failed to encode contracts: %w", err)
	}

	return nil
}

// QRCodeService handles QR code generation
type QRCodeService struct{}

// NewQRCodeService creates a new QR code service
func NewQRCodeService() *QRCodeService {
	return &QRCodeService{}
}

// GenerateQRCode generates a QR code for given address and amount
func (s *QRCodeService) GenerateQRCode(address, amount string) ([]byte, error) {
	// Generate QR code
	qr, err := qrcode.New(address+"?amount="+amount, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Convert to PNG
	buf := new(bytes.Buffer)
	err = png.Encode(buf, qr.Image(256))
	if err != nil {
		return nil, fmt.Errorf("failed to encode QR code to PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// HealthService handles health check business logic
type HealthService struct{}

// NewHealthService creates a new health service
func NewHealthService() *HealthService {
	return &HealthService{}
}

// GetHealthStatus returns current health status
func (s *HealthService) GetHealthStatus() *models.HealthResponse {
	return &models.HealthResponse{
		Status:    "healthy",
		Message:   "Backend is running (restored version with full functionality)",
		Timestamp: time.Now().Unix(),
	}
}
