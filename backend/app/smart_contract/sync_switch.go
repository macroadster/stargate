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
// warns and does not decide, so the next name is still consulted and only a
// genuinely undecided pair reaches the default. A typo in one name therefore
// neither disables sync nor overrides a real value under the other; see
// syncEnabledFrom for why the second half matters.
func syncEnabled() bool {
	// The canonical name is consulted first, so a deployment can migrate by
	// adding it without first removing the old one.
	if enabled, ok := syncEnabledFrom(SyncEnabledEnv); ok {
		return enabled
	}
	if enabled, ok := syncEnabledFrom(syncEnabledLegacyEnv); ok {
		syncLegacyWarnOnce.Do(func() {
			log.Printf("%s is deprecated; use %s. Honouring it for now.", syncEnabledLegacyEnv, SyncEnabledEnv)
		})
		return enabled
	}
	return true
}

// syncEnabledFrom reads one variable, reporting whether it decided the question.
//
// A value that is set but not understood does not decide it. That matters
// because of how the two rules interact: with precedence alone, a typo in the
// canonical name would take priority over a working legacy setting and, since an
// unparseable value must not disable sync, would silently turn it back on for a
// deployment that had turned it off. Declining to decide lets the legacy value
// still be honoured, and only a genuinely unset pair falls through to the
// default.
func syncEnabledFrom(name string) (enabled, decided bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false
	}
	enabled, ok := parseBool(raw)
	if !ok {
		log.Printf("%s=%q is not a recognised boolean and is being ignored. Use true or false.", name, raw)
		return false, false
	}
	return enabled, true
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
