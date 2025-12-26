package smart_contract

import (
	"context"
	"fmt"
	"strings"
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
type SecurityContext struct {
	ClientID     string
	APIKey       string
	RequestCount int
	LastRequest  time.Time
	Suspicious   bool
	Blocked      bool
}

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
func (sm *SecurityManager) MarkSuspicious(ipAddr string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.suspiciousIPs[ipAddr] = time.Now()

	// Auto-block if too many suspicious activities
	suspiciousCount := 0
	for ip := range sm.suspiciousIPs {
		if ip == ipAddr {
			suspiciousCount++
		}
	}

	if suspiciousCount > 5 {
		sm.blockedIPs[ipAddr] = time.Now()
	}
}

// GetSecurityStatus returns security status for monitoring
func (sm *SecurityManager) GetSecurityStatus() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]interface{}{
		"suspicious_ips": len(sm.suspiciousIPs),
		"blocked_ips":    len(sm.blockedIPs),
		"active_clients": len(sm.rateLimiter.clients),
	}
}

// Global security manager instance
var GlobalSecurityManager = NewSecurityManager()

// SecurityMiddleware provides security validation for API calls
func SecurityMiddleware(ctx context.Context, clientID, apiKey, ipAddr string) error {
	return GlobalSecurityManager.ValidateRequest(ctx, clientID, apiKey, ipAddr)
}

// ValidateAPIRequest validates common API request parameters
func ValidateAPIRequest(method, endpoint string, headers map[string]string) error {
	// Validate HTTP method
	allowedMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	methodValid := false
	for _, allowed := range allowedMethods {
		if method == allowed {
			methodValid = true
			break
		}
	}
	if !methodValid {
		return fmt.Errorf("HTTP method %s not allowed", method)
	}

	// Validate endpoint doesn't contain path traversal
	if strings.Contains(endpoint, "..") || strings.Contains(endpoint, "%2e%2e") {
		return fmt.Errorf("path traversal detected in endpoint")
	}

	// Check for suspicious headers
	suspiciousHeaders := []string{"x-forwarded-for", "x-real-ip", "x-originating-ip"}
	for _, header := range suspiciousHeaders {
		if value, exists := headers[header]; exists {
			if strings.Contains(value, "127.0.0.1") || strings.Contains(value, "::1") {
				return fmt.Errorf("suspicious header value detected")
			}
		}
	}

	return nil
}

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
func (al *AuditLogger) GetRecentEvents(limit int) []AuditEvent {
	al.mu.RLock()
	defer al.mu.RUnlock()

	if limit > len(al.events) {
		limit = len(al.events)
	}

	return al.events[len(al.events)-limit:]
}

// Global audit logger
var GlobalAuditLogger = NewAuditLogger()

// LogSecurityEvent logs a security event
func LogSecurityEvent(eventType, clientID, ipAddr, description, severity string) {
	GlobalAuditLogger.LogEvent(eventType, clientID, ipAddr, description, severity)
}
