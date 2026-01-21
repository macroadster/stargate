package smart_contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/ipfs"
)

type syncPubsubConfig struct {
	Enabled        bool
	Topic          string
	APIURL         string
	Interval       time.Duration
	Issuer         string
	DedupeInterval time.Duration
}

// StartSyncPubsubSync listens for sync announcements and applies them locally.
func StartSyncPubsubSync(ctx context.Context, server *Server) error {
	if server == nil {
		return fmt.Errorf("sync pubsub requires server")
	}
	cfg := loadSyncPubsubConfig()
	if !cfg.Enabled {
		log.Printf("mcp sync pubsub disabled")
		return nil
	}
	log.Printf("mcp sync pubsub enabled: topic=%s, api_url=%s, issuer=%s", cfg.Topic, cfg.APIURL, cfg.Issuer)

	streamClient := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			IdleConnTimeout:   90 * time.Second,
			DisableKeepAlives: false,
		},
	}

	go func() {
		for {
			if err := syncPubsubSubscribe(ctx, server, cfg, streamClient); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("mcp sync pubsub error: %v, retrying in %v", err, cfg.Interval)
				time.Sleep(cfg.Interval)
			}
		}
	}()
	return nil
}

// Global deduplication state to prevent sync storms
var (
	lastSyncTime = make(map[string]time.Time)
	syncMutex    sync.RWMutex
)

func loadSyncPubsubConfig() syncPubsubConfig {
	enabled := true
	if raw := os.Getenv("STARGATE_SYNC_ENABLED"); raw != "" {
		enabled = strings.EqualFold(raw, "true")
	}
	apiURL := os.Getenv("IPFS_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:5001"
	}
	// Use existing stargate-stego topic for sync announcements
	topic := os.Getenv("IPFS_SYNC_TOPIC")
	if topic == "" {
		topic = "stargate-stego"
	}
	interval := 10 * time.Second
	if raw := os.Getenv("IPFS_SYNC_INTERVAL_SEC"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			interval = time.Duration(v) * time.Second
		}
	}
	issuer := os.Getenv("STARGATE_INSTANCE_ID")
	if issuer == "" {
		if host, err := os.Hostname(); err == nil {
			issuer = host
		} else {
			issuer = "stargate-node"
		}
	}

	return syncPubsubConfig{
		Enabled:        enabled,
		Topic:          topic,
		APIURL:         apiURL,
		Interval:       interval,
		Issuer:         issuer,
		DedupeInterval: 5 * time.Second, // 5-second deduplication window
	}
}

func syncPubsubSubscribe(ctx context.Context, server *Server, cfg syncPubsubConfig, client *http.Client) error {
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/sub?arg=%s", strings.TrimRight(cfg.APIURL, "/"), url.QueryEscape(multibaseEncodeString(cfg.Topic)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) == 0 {
			return fmt.Errorf("pubsub subscribe failed: %s", resp.Status)
		}
		return fmt.Errorf("pubsub subscribe failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var msg pubsubMessage
		if err := decoder.Decode(&msg); err != nil {
			return err
		}

		// Try to parse as sync announcement first, then as stego announcement
		candidates := make([][]byte, 0, 3)
		candidates = append(candidates, []byte(msg.Data))
		if decoded := decodeMultibasePayload([]byte(msg.Data)); len(decoded) > 0 {
			candidates = append(candidates, decoded)
		}
		if decoded := decodeBase64Payload(msg.Data); len(decoded) > 0 {
			candidates = append(candidates, decoded)
			if decoded2 := decodeMultibasePayload(decoded); len(decoded2) > 0 {
				candidates = append(candidates, decoded2)
			}
		}

		var ann syncAnnouncement
		parsed := false
		for _, payload := range candidates {
			payload = bytes.TrimSpace(payload)
			if len(payload) == 0 {
				continue
			}
			if err := json.Unmarshal(payload, &ann); err == nil && ann.Type != "" {
				parsed = true
				break
			}
		}

		if !parsed {
			continue // Not a sync announcement, skip
		}

		if ann.Issuer == cfg.Issuer {
			continue // Skip our own announcements
		}

		// Create deduplication key based on announcement content
		dedupeKey := fmt.Sprintf("%s:%s:%d", ann.Type, ann.Issuer, ann.Timestamp)

		// Check if we've recently processed this announcement
		syncMutex.RLock()
		if lastProcessed, exists := lastSyncTime[dedupeKey]; exists {
			if time.Since(lastProcessed) < cfg.DedupeInterval {
				syncMutex.RUnlock()
				log.Printf("sync deduplication: skipping duplicate announcement from %s", ann.Issuer)
				continue
			}
		}
		syncMutex.RUnlock()

		// Mark this announcement as processed
		syncMutex.Lock()
		lastSyncTime[dedupeKey] = time.Now()
		syncMutex.Unlock()

		if err := server.ReconcileSyncAnnouncement(ctx, &ann); err != nil {
			log.Printf("sync reconcile failed: %v", err)
		}
	}
}

func decodePubsubPayload(data []byte) []byte {
	// Simple decoding, can be expanded if using multibase
	return data
}

// PublishSyncAnnouncement broadcasts a sync message to the cluster.
func (s *Server) PublishSyncAnnouncement(ctx context.Context, ann *syncAnnouncement) error {
	cfg := loadSyncPubsubConfig()
	if !cfg.Enabled || ann == nil {
		return nil
	}
	ann.Issuer = cfg.Issuer
	ann.Timestamp = time.Now().Unix()

	// Create deduplication key to check if we recently published this
	dedupeKey := fmt.Sprintf("%s:%s:%d", ann.Type, ann.Issuer, ann.Timestamp)

	// Check if we recently published this exact announcement
	syncMutex.RLock()
	if lastPublished, exists := lastSyncTime[dedupeKey]; exists {
		if time.Since(lastPublished) < cfg.DedupeInterval {
			syncMutex.RUnlock()
			log.Printf("sync publish deduplication: skipping recent republication of %s", ann.Type)
			return nil
		}
	}
	syncMutex.RUnlock()

	// Mark as published before actual publish to prevent race conditions
	syncMutex.Lock()
	lastSyncTime[dedupeKey] = time.Now()
	syncMutex.Unlock()

	data, err := json.Marshal(ann)
	if err != nil {
		return err
	}

	log.Printf("publishing sync announcement: type=%s, issuer=%s", ann.Type, ann.Issuer)

	// Use IPFS client for proper multipart form publishing
	client := ipfs.NewClientFromEnv()
	return client.PubsubPublish(ctx, cfg.Topic, data)
}
