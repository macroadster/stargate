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
	"time"

	"stargate-backend/ipfs"
)

type stegoPubsubConfig struct {
	Enabled  bool
	Topic    string
	APIURL   string
	Interval time.Duration
}

type stegoPubsubState struct {
	lastSeen map[string]int64
}

// StartStegoPubsubSync listens for stego announcements and reconciles them locally.
func StartStegoPubsubSync(ctx context.Context, server *Server) error {
	if server == nil {
		return fmt.Errorf("stego pubsub sync requires server")
	}
	cfg := loadStegoPubsubConfig()
	if !cfg.Enabled {
		log.Printf("stego pubsub sync disabled")
		return nil
	}
	log.Printf("stego pubsub sync enabled: topic=%s", cfg.Topic)
	state := &stegoPubsubState{lastSeen: make(map[string]int64)}
	go func() {
		for {
			if err := stegoPubsubSubscribe(ctx, server, cfg, state); err != nil {
				if ctx.Err() != nil {
					log.Printf("stego pubsub sync stopped: %v", err)
					return
				}
				log.Printf("stego pubsub sync error: %v, retrying in %v", err, cfg.Interval)
				time.Sleep(cfg.Interval)
			}
		}
	}()
	return nil
}

func loadStegoPubsubConfig() stegoPubsubConfig {
	enabled := true
	if raw := strings.TrimSpace(os.Getenv("IPFS_STEGO_SYNC_ENABLED")); raw != "" {
		enabled = strings.EqualFold(raw, "true")
	}
	interval := 10 * time.Second
	if raw := strings.TrimSpace(os.Getenv("IPFS_STEGO_SYNC_INTERVAL_SEC")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			interval = time.Duration(v) * time.Second
		}
	}
	topic := strings.TrimSpace(os.Getenv("IPFS_STEGO_TOPIC"))
	if topic == "" {
		topic = "stargate-stego"
	}

	if enabled {
		client := ipfs.NewClientFromEnv()
		if client == nil {
			enabled = false
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := client.CheckNode(ctx); err != nil {
				log.Printf("stego pubsub sync: local IPFS node not reachable (%v), disabling pubsub sync", err)
				enabled = false
			}
		}
	}

	return stegoPubsubConfig{
		Enabled:  enabled,
		Topic:    topic,
		APIURL:   "",
		Interval: interval,
	}
}

func stegoPubsubSubscribe(ctx context.Context, server *Server, cfg stegoPubsubConfig, state *stegoPubsubState) error {
	client := ipfs.NewClientFromEnv()
	if client == nil {
		return fmt.Errorf("IPFS client not available")
	}

	ch, err := client.PubsubSubscribe(ctx, cfg.Topic)
	if err != nil {
		log.Printf("stego pubsub subscribe failed: %v", err)
		return err
	}
	log.Printf("stego pubsub subscribed to %s, waiting for messages...", cfg.Topic)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("pubsub channel closed")
			}
			announcement, err := parseStegoAnnouncement(string(msg))
			if err != nil {
				log.Printf("stego pubsub decode failed: %v", err)
				continue
			}
			if announcement == nil || strings.TrimSpace(announcement.StegoCID) == "" {
				continue
			}
			seenAt := announcement.Timestamp
			if seenAt <= 0 {
				seenAt = time.Now().Unix()
			}
			if last, ok := state.lastSeen[announcement.StegoCID]; ok && seenAt <= last {
				continue
			}
			if err := server.ReconcileStegoWithAnnouncement(ctx, announcement); err != nil {
				log.Printf("stego pubsub reconcile failed for %s: %v", announcement.StegoCID, err)
				continue
			}
			state.lastSeen[announcement.StegoCID] = seenAt
			log.Printf("stego pubsub reconciled: stego_cid=%s", announcement.StegoCID)
		}
	}
}

func parseStegoAnnouncement(encoded string) (*stegoAnnouncement, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}

	candidates := make([][]byte, 0, 3)
	candidates = append(candidates, []byte(encoded))
	if decoded := decodeMultibasePayload([]byte(encoded)); len(decoded) > 0 {
		candidates = append(candidates, decoded)
	}
	if decoded := decodeBase64Payload(encoded); len(decoded) > 0 {
		candidates = append(candidates, decoded)
		if decoded2 := decodeMultibasePayload(decoded); len(decoded2) > 0 {
			candidates = append(candidates, decoded2)
		}
	}

	for _, payload := range candidates {
		payload = bytes.TrimSpace(payload)
		if len(payload) == 0 {
			continue
		}
		var ann stegoAnnouncement
		if err := json.Unmarshal(payload, &ann); err == nil && strings.TrimSpace(ann.StegoCID) != "" {
			return &ann, nil
		}
	}
	return nil, nil
}
