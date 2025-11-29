package starlight

import (
	"fmt"
	"log"
	"sync"
	"time"

	"stargate-backend/core"
)

// ScannerManager manages a single scanner instance with circuit breaker protection
type ScannerManager struct {
	scanner        core.StarlightScannerInterface
	circuitBreaker *CircuitBreaker
	mutex          sync.RWMutex
	initialized    bool
	scannerType    string
}

// CircuitBreaker implements circuit breaker pattern for resilience
type CircuitBreaker struct {
	failures    int
	lastFailure time.Time
	state       string // "closed", "open", "half-open"
	maxFailures int
	timeout     time.Duration
	mutex       sync.RWMutex
}

var (
	globalScannerManager *ScannerManager
	once                 sync.Once
)

// GetScannerManager returns singleton scanner manager instance
func GetScannerManager() *ScannerManager {
	once.Do(func() {
		globalScannerManager = &ScannerManager{
			circuitBreaker: NewCircuitBreaker(3, 30*time.Second),
			initialized:    false,
		}
	})
	return globalScannerManager
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failures:    0,
		maxFailures: maxFailures,
		timeout:     timeout,
		state:       "closed",
	}
}

// InitializeScanner initializes the scanner with proper fallback logic
func (sm *ScannerManager) InitializeScanner() error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.initialized {
		return nil
	}

	// Try to initialize proxy scanner first
	proxyScanner := NewProxyScanner("http://localhost:8080", "demo-api-key")
	if proxyScanner != nil {
		initErr := proxyScanner.Initialize()
		if initErr == nil {
			sm.scanner = proxyScanner
			sm.scannerType = "proxy"
			sm.initialized = true
			log.Printf("Initialized proxy scanner (Python API)")
			return nil
		}
		log.Printf("Proxy scanner initialization failed: %v, falling back to mock scanner", initErr)
	}

	// Fallback to mock scanner
	sm.scanner = NewMockStarlightScanner()
	sm.scannerType = "mock"
	sm.initialized = true
	log.Printf("Initialized mock scanner")

	return nil
}

// ScanImage scans an image with circuit breaker protection
func (sm *ScannerManager) ScanImage(imageData []byte, options core.ScanOptions) (*core.ScanResult, error) {
	if !sm.initialized {
		if err := sm.InitializeScanner(); err != nil {
			return nil, fmt.Errorf("scanner not initialized: %v", err)
		}
	}

	if !sm.circuitBreaker.CanExecute() {
		return &core.ScanResult{
			IsStego:          false,
			StegoProbability: 0.0,
			Confidence:       0.0,
			Prediction:       "circuit_breaker_open",
		}, fmt.Errorf("circuit breaker open")
	}

	result, err := sm.scanner.ScanImage(imageData, options)
	if err != nil {
		sm.circuitBreaker.RecordFailure()
		return nil, err
	}

	sm.circuitBreaker.RecordSuccess()
	return result, nil
}

// GetScannerType returns type of scanner being used
func (sm *ScannerManager) GetScannerType() string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.scannerType
}

// GetHealthStatus returns health status of scanner manager
func (sm *ScannerManager) GetHealthStatus() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	status := map[string]interface{}{
		"initialized":  sm.initialized,
		"scanner_type": sm.scannerType,
		"circuit_breaker": map[string]interface{}{
			"state":    sm.circuitBreaker.GetState(),
			"failures": sm.circuitBreaker.GetFailures(),
		},
	}

	if sm.scanner != nil {
		status["scanner_healthy"] = sm.scanner.IsInitialized()
		scannerInfo := sm.scanner.GetScannerInfo()
		status["scanner_info"] = scannerInfo
	}

	return status
}

// GetScannerInfo returns scanner info from underlying scanner
func (sm *ScannerManager) GetScannerInfo() core.ScannerInfo {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	if sm.scanner != nil {
		return sm.scanner.GetScannerInfo()
	}

	return core.ScannerInfo{
		ModelLoaded:  false,
		ModelVersion: "unknown",
		ModelPath:    "none",
		Device:       "none",
	}
}

// IsInitialized returns initialization status
func (sm *ScannerManager) IsInitialized() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.initialized
}

// ExtractMessage extracts hidden message using underlying scanner
func (sm *ScannerManager) ExtractMessage(imageData []byte, method string) (*core.ExtractionResult, error) {
	if !sm.initialized {
		if err := sm.InitializeScanner(); err != nil {
			return nil, fmt.Errorf("scanner not initialized: %v", err)
		}
	}

	if !sm.circuitBreaker.CanExecute() {
		return &core.ExtractionResult{
			MessageFound: false,
			ExtractionDetails: map[string]interface{}{
				"error": "circuit breaker open",
			},
		}, fmt.Errorf("circuit breaker open")
	}

	result, err := sm.scanner.ExtractMessage(imageData, method)
	if err != nil {
		sm.circuitBreaker.RecordFailure()
		return nil, err
	}

	sm.circuitBreaker.RecordSuccess()
	return result, nil
}

// CanExecute checks if circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case "closed":
		return true
	case "open":
		return time.Since(cb.lastFailure) > cb.timeout
	case "half-open":
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failures = 0
	cb.state = "closed"
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = "open"
	}
}

// GetState returns current state of circuit breaker
func (cb *CircuitBreaker) GetState() string {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	// Auto-transition from open to half-open after timeout
	if cb.state == "open" && time.Since(cb.lastFailure) > cb.timeout {
		cb.state = "half-open"
	}

	return cb.state
}

// GetFailures returns current failure count
func (cb *CircuitBreaker) GetFailures() int {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.failures
}
