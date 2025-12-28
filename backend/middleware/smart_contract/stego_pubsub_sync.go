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
	"time"
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
		return nil
	}
	state := &stegoPubsubState{lastSeen: make(map[string]int64)}
	streamClient := &http.Client{}
	go func() {
		for {
			if err := stegoPubsubSubscribe(ctx, server, cfg, state, streamClient); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("stego pubsub sync error: %v", err)
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
	apiURL := strings.TrimSpace(os.Getenv("IPFS_API_URL"))
	if apiURL == "" {
		apiURL = "http://127.0.0.1:5001"
	}
	topic := strings.TrimSpace(os.Getenv("IPFS_STEGO_TOPIC"))
	if topic == "" {
		topic = "stargate-stego"
	}
	return stegoPubsubConfig{
		Enabled:  enabled,
		Topic:    topic,
		APIURL:   apiURL,
		Interval: interval,
	}
}

func stegoPubsubSubscribe(ctx context.Context, server *Server, cfg stegoPubsubConfig, state *stegoPubsubState, streamClient *http.Client) error {
	reqURL := fmt.Sprintf("%s/api/v0/pubsub/sub?arg=%s", strings.TrimRight(cfg.APIURL, "/"), url.QueryEscape(multibaseEncodeString(cfg.Topic)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := streamClient.Do(req)
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
		announcement, err := parseStegoAnnouncement(msg.Data)
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
		if err := server.ReconcileStego(ctx, announcement.StegoCID, announcement.ExpectedHash); err != nil {
			log.Printf("stego pubsub reconcile failed for %s: %v", announcement.StegoCID, err)
			continue
		}
		state.lastSeen[announcement.StegoCID] = seenAt
		log.Printf("stego pubsub reconciled: stego_cid=%s", announcement.StegoCID)
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
