package agents

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime configuration for the built-in agent orchestrator.
// Values are populated from environment variables (STARGATE_AGENT_* preferred,
// with some legacy STARLIGHT_* fallbacks for transition compatibility).
type Config struct {
	Enabled               bool
	WatcherEnabled        bool
	WorkerEnabled         bool
	AIIdentifier          string
	PollInterval          time.Duration
	DonationAddress       string
	UploadsDir            string
	MaxProposalsPerCycle  int
	MaxProposalsPerWish   int
	MaxCycles             int
}

// DefaultConfig returns reasonable defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		WatcherEnabled:       true,
		WorkerEnabled:        true,
		AIIdentifier:         "stargate-builtin-agent",
		PollInterval:         60 * time.Second,
		UploadsDir:           "/data/uploads",
		MaxProposalsPerCycle: 1,
		MaxProposalsPerWish:  5,
		MaxCycles:            10000,
	}
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	cfg := DefaultConfig()

	if v := os.Getenv("STARGATE_AGENT_ENABLED"); v != "" {
		cfg.Enabled = strings.ToLower(v) != "false"
	}
	if v := os.Getenv("STARGATE_AGENT_WATCHER_ENABLED"); v != "" {
		cfg.WatcherEnabled = strings.ToLower(v) != "false"
	} else if v := os.Getenv("STARLIGHT_WATCHER_ENABLED"); v != "" {
		cfg.WatcherEnabled = strings.ToLower(v) != "false"
	}
	if v := os.Getenv("STARGATE_AGENT_WORKER_ENABLED"); v != "" {
		cfg.WorkerEnabled = strings.ToLower(v) != "false"
	} else if v := os.Getenv("STARLIGHT_WORKER_ENABLED"); v != "" {
		cfg.WorkerEnabled = strings.ToLower(v) != "false"
	}

	if v := os.Getenv("STARGATE_AGENT_AI_IDENTIFIER"); v != "" {
		cfg.AIIdentifier = v
	} else if v := os.Getenv("AI_IDENTIFIER"); v != "" {
		cfg.AIIdentifier = v
	}

	if v := os.Getenv("STARGATE_AGENT_POLL_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			cfg.PollInterval = time.Duration(secs) * time.Second
		}
	} else if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			cfg.PollInterval = time.Duration(secs) * time.Second
		}
	}

	if v := os.Getenv("STARLIGHT_DONATION_ADDRESS"); v != "" {
		cfg.DonationAddress = strings.TrimSpace(v)
	}

	if v := os.Getenv("UPLOADS_DIR"); v != "" {
		cfg.UploadsDir = v
	}

	if v := os.Getenv("STARGATE_AGENT_MAX_PROPOSALS_PER_CYCLE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxProposalsPerCycle = n
		}
	}
	if v := os.Getenv("STARGATE_AGENT_MAX_PROPOSALS_PER_WISH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxProposalsPerWish = n
		}
	}
	if v := os.Getenv("STARGATE_AGENT_MAX_CYCLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxCycles = n
		}
	}

	return cfg
}
