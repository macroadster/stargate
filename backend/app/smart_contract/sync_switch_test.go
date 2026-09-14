package smart_contract

import (
	"context"
	"math"
	"testing"

	"stargate-backend/core/smart_contract"
)

// z6k: sync_pubsub.go read STARGATE_SYNC_ENABLED and treated anything but "true"
// as off; ingestion_sync.go read STARGATE_SYNC_ENABLE and treated only "false" as
// off. No single variable turned both off, and the value rules disagreed too.
func TestSyncEnabled(t *testing.T) {
	cases := []struct {
		name      string
		canonical string // STARGATE_SYNC_ENABLED
		legacy    string // STARGATE_SYNC_ENABLE
		want      bool
	}{
		{name: "unset is enabled, as both call sites already assumed", want: true},

		{name: "canonical false disables", canonical: "false", want: false},
		{name: "canonical true enables", canonical: "true", want: true},

		// The old pubsub rule was EqualFold(raw, "true"), so 1/yes/on all read as
		// "disable" there while meaning nothing at all to the other call site.
		{name: "canonical 1 enables", canonical: "1", want: true},
		{name: "canonical yes enables", canonical: "yes", want: true},
		{name: "canonical on enables", canonical: "on", want: true},
		{name: "canonical 0 disables", canonical: "0", want: false},
		{name: "canonical no disables", canonical: "no", want: false},
		{name: "canonical off disables", canonical: "off", want: false},
		{name: "canonical FALSE disables regardless of case", canonical: "FALSE", want: false},
		{name: "canonical is trimmed", canonical: "  false  ", want: false},

		// The old spelling keeps working, so upgrading the binary does not
		// silently switch sync back on for a deployment that had turned it off.
		{name: "legacy false still disables", legacy: "false", want: false},
		{name: "legacy 0 disables, which the old rule ignored", legacy: "0", want: false},
		{name: "legacy true enables", legacy: "true", want: true},

		// Canonical wins, so a deployment can add the new name before removing
		// the old one without a window where they fight.
		{name: "canonical false beats legacy true", canonical: "false", legacy: "true", want: false},
		{name: "canonical true beats legacy false", canonical: "true", legacy: "false", want: true},

		// A typo must not be the thing that silently stops sync.
		{name: "unparseable canonical stays enabled", canonical: "maybe", want: true},
		{name: "unparseable legacy stays enabled", legacy: "disabled", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STARGATE_SYNC_ENABLED", tc.canonical)
			t.Setenv("STARGATE_SYNC_ENABLE", tc.legacy)
			if got := syncEnabled(); got != tc.want {
				t.Errorf("syncEnabled() = %v, want %v (STARGATE_SYNC_ENABLED=%q STARGATE_SYNC_ENABLE=%q)",
					got, tc.want, tc.canonical, tc.legacy)
			}
		})
	}
}

// unpublishableProposal cannot be JSON encoded, which is how the call site below
// is observed without involving IPFS at all.
//
// publishProposalEvent consults the switch, then marshals, then builds an IPFS
// client. Marshalling a NaN fails, so the switch decision shows up as: disabled
// returns a clean nil, enabled returns the marshal error. Going through IPFS
// instead is not usable here, because ipfs.NewClientFromEnv memoises through a
// sync.Once and what it returns depends on what else in the package ran first.
func unpublishableProposal() smart_contract.Proposal {
	return smart_contract.Proposal{
		ID:       "p-1",
		Metadata: map[string]any{"unserialisable": math.NaN()},
	}
}

// TestOneSwitchStopsProposalPublish is the bead's operator-facing claim on this
// call site: either spelling turns it off. Before this, STARGATE_SYNC_ENABLED had
// no effect here whatsoever, because this path read only the other spelling.
func TestOneSwitchStopsProposalPublish(t *testing.T) {
	publish := func() error {
		return publishProposalEvent(context.Background(), unpublishableProposal())
	}

	// Positive control. Without it, a disabled nil would be indistinguishable
	// from a call site that ignores the switch and happens to return nil anyway.
	// An earlier version of this test lacked this and survived a mutation that
	// restored both of the old switches.
	t.Run("control: switch unset means the publish is attempted", func(t *testing.T) {
		t.Setenv("STARGATE_SYNC_ENABLED", "")
		t.Setenv("STARGATE_SYNC_ENABLE", "")
		if err := publish(); err == nil {
			t.Fatal("publish returned nil with the switch unset, so the assertions below prove nothing")
		}
	})

	for _, envVar := range []string{"STARGATE_SYNC_ENABLED", "STARGATE_SYNC_ENABLE"} {
		t.Run(envVar+"=false stops it", func(t *testing.T) {
			t.Setenv("STARGATE_SYNC_ENABLED", "")
			t.Setenv("STARGATE_SYNC_ENABLE", "")
			t.Setenv(envVar, "false")

			if err := publish(); err != nil {
				t.Errorf("publish was attempted with %s=false: %v", envVar, err)
			}
		})
	}

	// Old rule: anything not literally "true" meant disable. This is where the
	// old and new value rules disagree in the enabling direction.
	t.Run("STARGATE_SYNC_ENABLED=1 leaves it enabled", func(t *testing.T) {
		t.Setenv("STARGATE_SYNC_ENABLED", "1")
		t.Setenv("STARGATE_SYNC_ENABLE", "")

		if err := publish(); err == nil {
			t.Error("STARGATE_SYNC_ENABLED=1 read as disabled; 1 is not a way of saying no")
		}
	})
}

// TestPubsubConfigConsultsTheSwitch is deliberately limited, and the limit is
// the point.
//
// loadSyncPubsubConfig only ever reports enabled when an IPFS node answers
// CheckNode within 2s, and it forces disabled when the embedded node is in use.
// With no node in a unit test it returns false whatever the switch says, so
// "enabled" is not assertable here and a test that only ever checks for false
// would pass against any implementation. The switch itself is covered by
// TestSyncEnabled above; this pins only that the disable path is reachable.
func TestPubsubConfigConsultsTheSwitch(t *testing.T) {
	t.Setenv("IPFS_ENABLED", "true")
	t.Setenv("IPFS_EMBEDDED_ENABLED", "false")
	t.Setenv("STARGATE_SYNC_ENABLED", "false")
	t.Setenv("STARGATE_SYNC_ENABLE", "")

	if loadSyncPubsubConfig().Enabled {
		t.Error("pubsub sync enabled with STARGATE_SYNC_ENABLED=false")
	}
}
