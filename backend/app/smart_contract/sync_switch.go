package smart_contract

import (
	"log"
	"os"
	"strings"
	"sync"
)

// SyncEnabledEnv is the one variable that turns IPFS sync publishing on or off.
const SyncEnabledEnv = "STARGATE_SYNC_ENABLED"

// syncEnabledLegacyEnv is the older spelling, one character shorter, kept
// working so existing deployments are not silently switched on.
const syncEnabledLegacyEnv = "STARGATE_SYNC_ENABLE"

var syncLegacyWarnOnce sync.Once

// syncEnabled reports whether sync publishing is enabled.
//
// This used to be two switches that neither shared a name nor agreed on what
// their values meant (stargate-z6k):
//
//	sync_pubsub.go     STARGATE_SYNC_ENABLED, off unless the value equalled "true"
//	ingestion_sync.go  STARGATE_SYNC_ENABLE,  off only if the value equalled "false"
//
// So no single variable turned both off, and the two disagreed about values as
// well as names: STARGATE_SYNC_ENABLED=1 disabled the pubsub loop, because 1 is
// not "true", while leaving proposal event publishing running, because it read
// the other name entirely. An operator had no way to turn sync off.
//
// Unset means enabled, which both call sites already did. An unparseable value
// warns and falls back to that default rather than disabling: silently not
// syncing is the failure mode that is hard to notice, and a typo should not
// produce it.
func syncEnabled() bool {
	raw, name := syncEnabledRaw()
	if raw == "" {
		return true
	}
	enabled, ok := parseBool(raw)
	if !ok {
		log.Printf("%s=%q is not a recognised boolean; sync stays enabled. Use true or false.", name, raw)
		return true
	}
	return enabled
}

// syncEnabledRaw returns the value in effect and the variable it came from. The
// canonical name wins when both are set, so a deployment can migrate by adding
// the new name without first removing the old one.
func syncEnabledRaw() (value, name string) {
	if raw := strings.TrimSpace(os.Getenv(SyncEnabledEnv)); raw != "" {
		return raw, SyncEnabledEnv
	}
	if raw := strings.TrimSpace(os.Getenv(syncEnabledLegacyEnv)); raw != "" {
		syncLegacyWarnOnce.Do(func() {
			log.Printf("%s is deprecated; use %s. Honouring it for now.", syncEnabledLegacyEnv, SyncEnabledEnv)
		})
		return raw, syncEnabledLegacyEnv
	}
	return "", SyncEnabledEnv
}

// parseBool accepts the spellings people actually put in environment files.
// The old code accepted only the exact words "true" and "false", in opposite
// directions in the two places, so 1, yes and on all read as "disable" in one of
// them.
func parseBool(raw string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, true
	case "0", "f", "false", "n", "no", "off":
		return false, true
	}
	return false, false
}
