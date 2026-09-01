package smart_contract

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	clients map[string]*ClientBucket
	mu      sync.RWMutex
}

// ClientBucket tracks rate limit state for a client
type ClientBucket struct {
	tokens     int
	lastRefill time.Time
	capacity   int
	refillRate int // tokens per second
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*ClientBucket),
	}
}

// CheckRateLimit checks if a request should be allowed
func (rl *RateLimiter) CheckRateLimit(clientID string, cost int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.clients[clientID]
	if !exists {
		// Create new bucket with default settings
		bucket = &ClientBucket{
			tokens:     100, // Start with full bucket
			capacity:   100,
			refillRate: 10, // 10 tokens per second
			lastRefill: time.Now(),
		}
		rl.clients[clientID] = bucket
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed.Seconds()) * bucket.refillRate

	if tokensToAdd > 0 {
		bucket.tokens += tokensToAdd
		if bucket.tokens > bucket.capacity {
			bucket.tokens = bucket.capacity
		}
		bucket.lastRefill = now
	}

	// Check if enough tokens available
	if bucket.tokens >= cost {
		bucket.tokens -= cost
		return true
	}

	return false
}

// SecurityContext tracks security state for a request

// SecurityManager handles security policies and monitoring
type SecurityManager struct {
	rateLimiter   *RateLimiter
	suspiciousIPs map[string]time.Time
	blockedIPs    map[string]time.Time
	mu            sync.RWMutex
}

// NewSecurityManager creates a new security manager
func NewSecurityManager() *SecurityManager {
	return &SecurityManager{
		rateLimiter:   NewRateLimiter(),
		suspiciousIPs: make(map[string]time.Time),
		blockedIPs:    make(map[string]time.Time),
	}
}

// ValidateRequest validates a request against security policies
func (sm *SecurityManager) ValidateRequest(ctx context.Context, clientID, apiKey, ipAddr string) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Check if IP is blocked
	if blockedTime, exists := sm.blockedIPs[ipAddr]; exists {
		if time.Since(blockedTime) < time.Hour {
			return fmt.Errorf("IP address is temporarily blocked")
		}
		// Remove expired block
		delete(sm.blockedIPs, ipAddr)
	}

	// Check rate limiting
	if !sm.rateLimiter.CheckRateLimit(clientID, 1) {
		// Mark as suspicious
		sm.suspiciousIPs[ipAddr] = time.Now()
		return fmt.Errorf("rate limit exceeded")
	}

	// Validate API key format
	if err := ValidateAPIKeyFormat(apiKey); err != nil {
		sm.suspiciousIPs[ipAddr] = time.Now()
		return fmt.Errorf("invalid API key format: %v", err)
	}

	return nil
}

// MarkSuspicious marks an IP as suspicious

// GetSecurityStatus returns security status for monitoring

// Global security manager instance
var GlobalSecurityManager = NewSecurityManager()

// SecurityMiddleware provides security validation for API calls

// ValidateAPIRequest validates common API request parameters

// AuditLogger logs security events
type AuditLogger struct {
	events []AuditEvent
	mu     sync.RWMutex
}

// AuditEvent represents a security audit event
type AuditEvent struct {
	Timestamp   time.Time
	EventType   string
	ClientID    string
	IPAddr      string
	Description string
	Severity    string
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger() *AuditLogger {
	return &AuditLogger{
		events: make([]AuditEvent, 0),
	}
}

// LogEvent logs a security event
func (al *AuditLogger) LogEvent(eventType, clientID, ipAddr, description, severity string) {
	al.mu.Lock()
	defer al.mu.Unlock()

	event := AuditEvent{
		Timestamp:   time.Now(),
		EventType:   eventType,
		ClientID:    clientID,
		IPAddr:      ipAddr,
		Description: description,
		Severity:    severity,
	}

	al.events = append(al.events, event)

	// Keep only last 1000 events
	if len(al.events) > 1000 {
		al.events = al.events[1:]
	}
}

// GetRecentEvents returns recent audit events

// Global audit logger
var GlobalAuditLogger = NewAuditLogger()

// LogSecurityEvent logs a security event
