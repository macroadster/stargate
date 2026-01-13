package smart_contract

import (
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
	"time"
)

type syncPubsubConfig struct {
	Enabled  bool
	Topic    string
	APIURL   string
	Interval time.Duration
	Issuer   string
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

func loadSyncPubsubConfig() syncPubsubConfig {
	enabled := true
	if raw := os.Getenv("IPFS_SYNC_ENABLED"); raw != "" {
		enabled = strings.EqualFold(raw, "true")
	}
	apiURL := os.Getenv("IPFS_API_URL")
	if apiURL == "" {
		apiURL = "http://127.0.0.1:5001"
	}
	topic := os.Getenv("IPFS_SYNC_TOPIC")
	if topic == "" {
		topic = "stargate-mcp-sync"
	}
	interval := 5 * time.Second
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
		Enabled:  enabled,
		Topic:    topic,
		APIURL:   apiURL,
		Interval: interval,
		Issuer:   issuer,
	}
}

func syncPubsubSubscribe(ctx context.Context, server *Server, cfg syncPubsubConfig, client *http.Client) error {
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/sub?arg=%s", strings.TrimRight(cfg.APIURL, "/"), url.QueryEscape(cfg.Topic))
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
		return fmt.Errorf("pubsub subscribe failed: %s", resp.Status)
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Data []byte `json:"data"`
			From string `json:"from"`
		}
		if err := decoder.Decode(&msg); err != nil {
			return err
		}

		var ann syncAnnouncement
		// IPFS data is sometimes double-encoded or multibase
		payload := decodePubsubPayload(msg.Data)
		if err := json.Unmarshal(payload, &ann); err != nil {
			continue
		}

		if ann.Issuer == cfg.Issuer {
			continue // Skip our own announcements
		}

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

	data, err := json.Marshal(ann)
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("%s/api/v0/pubsub/pub?arg=%s&arg=%s", 
		strings.TrimRight(cfg.APIURL, "/"), 
		url.QueryEscape(cfg.Topic),
		url.QueryEscape(string(data)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pubsub publish failed: %s: %s", resp.Status, string(body))
	}

	return nil
}
