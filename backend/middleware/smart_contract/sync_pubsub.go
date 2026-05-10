package smart_contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"stargate-backend/storage/ipfs"
)

type syncPubsubConfig struct {
	Enabled        bool
	Topic          string
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
	log.Printf("mcp sync pubsub enabled: topic=%s, issuer=%s", cfg.Topic, cfg.Issuer)

	go func() {
		for {
			if err := syncPubsubSubscribe(ctx, server, cfg); err != nil {
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
	if !ipfs.IsEnabled() {
		enabled = false
	}
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

	if enabled {
		client := ipfs.NewClientFromEnv()
		if client == nil {
			enabled = false
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := client.CheckNode(ctx); err != nil {
				log.Printf("mcp sync pubsub: local IPFS node not reachable (%v), disabling pubsub sync", err)
				enabled = false
			}
		}
	}

	return syncPubsubConfig{
		Enabled:        enabled,
		Topic:          topic,
		Interval:       interval,
		Issuer:         issuer,
		DedupeInterval: 5 * time.Second,
	}
}

func syncPubsubSubscribe(ctx context.Context, server *Server, cfg syncPubsubConfig) error {
	client := ipfs.NewClientFromEnv()
	if client == nil {
		return fmt.Errorf("IPFS client not available")
	}

	ch, err := client.PubsubSubscribe(ctx, cfg.Topic)
	if err != nil {
		return err
	}
	log.Printf("mcp sync pubsub subscribed to %s, waiting for messages...", cfg.Topic)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("pubsub channel closed")
			}

			candidates := make([][]byte, 0, 3)
			candidates = append(candidates, []byte(msg))
			if decoded := decodeMultibasePayload([]byte(msg)); len(decoded) > 0 {
				candidates = append(candidates, decoded)
			}
			if decoded := decodeBase64Payload(string(msg)); len(decoded) > 0 {
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
				continue
			}

			if ann.Issuer == cfg.Issuer {
				continue
			}

			dedupeKey := fmt.Sprintf("%s:%s:%d", ann.Type, ann.Issuer, ann.Timestamp)

			syncMutex.RLock()
			if lastProcessed, exists := lastSyncTime[dedupeKey]; exists {
				if time.Since(lastProcessed) < cfg.DedupeInterval {
					syncMutex.RUnlock()
					log.Printf("sync deduplication: skipping duplicate announcement from %s", ann.Issuer)
					continue
				}
			}
			syncMutex.RUnlock()

			syncMutex.Lock()
			lastSyncTime[dedupeKey] = time.Now()
			syncMutex.Unlock()

			if err := server.ReconcileSyncAnnouncement(ctx, &ann); err != nil {
				log.Printf("sync reconcile failed: %v", err)
			}
		}
	}
}

func decodePubsubPayload(data []byte) []byte {
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

	dedupeKey := fmt.Sprintf("%s:%s:%d", ann.Type, ann.Issuer, ann.Timestamp)

	syncMutex.RLock()
	if lastPublished, exists := lastSyncTime[dedupeKey]; exists {
		if time.Since(lastPublished) < cfg.DedupeInterval {
			syncMutex.RUnlock()
			log.Printf("sync publish deduplication: skipping recent republication of %s", ann.Type)
			return nil
		}
	}
	syncMutex.RUnlock()

	syncMutex.Lock()
	lastSyncTime[dedupeKey] = time.Now()
	syncMutex.Unlock()

	data, err := json.Marshal(ann)
	if err != nil {
		return err
	}

	log.Printf("publishing sync announcement: type=%s, issuer=%s", ann.Type, ann.Issuer)

	client := ipfs.NewClientFromEnv()
	if client == nil {
		return fmt.Errorf("IPFS client is disabled")
	}
	return client.PubsubPublish(ctx, cfg.Topic, data)
}