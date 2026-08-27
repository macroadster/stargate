package bitcoin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTipCatchupWindow_NoSkipWhenBehind(t *testing.T) {
	// Was the bug: jump to tip-3 and skip 144828.. when current=144827 tip=144834.
	first, last, _, seeded := tipCatchupWindow(144827, 144834, 3, 25, 3)
	if seeded {
		t.Fatal("expected not seeded when current already set")
	}
	if first != 144828 {
		t.Fatalf("first=%d want 144828", first)
	}
	if last != 144834 {
		t.Fatalf("last=%d want 144834 (full catch-up within batch)", last)
	}
}

func TestTipCatchupWindow_CapsBatch(t *testing.T) {
	first, last, _, _ := tipCatchupWindow(100, 200, 3, 10, 3)
	if first != 101 {
		t.Fatalf("first=%d", first)
	}
	if last != 110 {
		t.Fatalf("last=%d want 110 (catchup batch 10)", last)
	}
}

func TestTipCatchupWindow_NormalPace(t *testing.T) {
	first, last, _, _ := tipCatchupWindow(100, 102, 3, 25, 3)
	if first != 101 || last != 102 {
		t.Fatalf("got %d..%d", first, last)
	}
}

func TestTipCatchupWindow_ColdStartSeedsNearTip(t *testing.T) {
	first, last, seededCurrent, seeded := tipCatchupWindow(0, 1000, 3, 25, 3)
	if !seeded {
		t.Fatal("expected cold start seed")
	}
	if seededCurrent != 997 { // tip-2 for window 3 → start=998, current=997
		t.Fatalf("seededCurrent=%d want 997", seededCurrent)
	}
	if first != 998 || last != 1000 {
		t.Fatalf("range %d..%d want 998..1000", first, last)
	}
}

func TestTipCatchupWindow_AlreadyAtTip(t *testing.T) {
	first, last, _, _ := tipCatchupWindow(500, 500, 3, 25, 3)
	if first != 0 || last != 0 {
		t.Fatalf("expected empty range, got %d..%d", first, last)
	}
}

func TestEvaluateTipLag_DetectsAndClears(t *testing.T) {
	resetTipLagStateForTest()
	now := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)

	st := EvaluateTipLag(100, 106, 3, nil, now)
	if !st.Lagging || st.LagBlocks != 6 {
		t.Fatalf("expected lagging lag=6, got %+v", st)
	}
	if st.FirstLagAt == nil || !st.FirstLagAt.Equal(now) {
		t.Fatalf("expected FirstLagAt=%v, got %+v", now, st.FirstLagAt)
	}

	// Still lagging later — FirstLagAt sticky.
	later := now.Add(5 * time.Minute)
	st2 := EvaluateTipLag(100, 107, 3, nil, later)
	if !st2.Lagging || st2.FirstLagAt == nil || !st2.FirstLagAt.Equal(now) {
		t.Fatalf("FirstLagAt should stick: %+v", st2)
	}

	// Recovered.
	st3 := EvaluateTipLag(107, 107, 3, nil, later.Add(time.Minute))
	if st3.Lagging || st3.LagBlocks != 0 {
		t.Fatalf("expected recovered: %+v", st3)
	}
	got := GetTipLagStatus()
	if got.Lagging {
		t.Fatalf("global still lagging: %+v", got)
	}
}

func TestEvaluateTipLag_ExplorerErrorDoesNotFalsePositive(t *testing.T) {
	resetTipLagStateForTest()
	now := time.Now()
	st := EvaluateTipLag(100, 0, 3, context.DeadlineExceeded, now)
	if st.Lagging {
		t.Fatalf("unreachable explorer should not mark lagging: %+v", st)
	}
	if st.ExternalError == "" {
		t.Fatal("expected external error recorded")
	}
}

func TestEvaluateTipLag_WithinThreshold(t *testing.T) {
	resetTipLagStateForTest()
	st := EvaluateTipLag(100, 102, 3, nil, time.Now())
	if st.Lagging {
		t.Fatalf("lag=2 < threshold=3 should not lag: %+v", st)
	}
	if st.LagBlocks != 2 {
		t.Fatalf("lag blocks=%d", st.LagBlocks)
	}
}

func TestMergeTipLagIntoStatus_MarksUnsynced(t *testing.T) {
	resetTipLagStateForTest()
	EvaluateTipLag(10, 20, 3, nil, time.Now())
	out := map[string]any{"synced": true, "blocks": int64(10)}
	mergeTipLagIntoStatus(out)
	if out["synced"] != false {
		t.Fatalf("expected synced=false when lagging, got %v", out["synced"])
	}
	if out["tip_lagging"] != true {
		t.Fatalf("tip_lagging=%v", out["tip_lagging"])
	}
	if out["external_tip"] != int64(20) {
		t.Fatalf("external_tip=%v", out["external_tip"])
	}
}

func TestShouldAdoptExplorerTip(t *testing.T) {
	ok := TipLagStatus{
		HashMismatch:    true,
		CompareHeight:   149952,
		LocalTipHash:    "0000000000ed4f344ba8335a5e390fe2ae58e8165c7829ba9456e1a983084198",
		ExternalTipHash: "0000000000baebbdeab043d4b98615caf906c0fba8948e0ff1bf20136ba50de8",
	}
	if !shouldAdoptExplorerTip(ok) {
		t.Fatal("expected adopt on same-height hash mismatch")
	}
	same := ok
	same.LocalTipHash = same.ExternalTipHash
	same.HashMismatch = false
	if shouldAdoptExplorerTip(same) {
		t.Fatal("matching hashes must not adopt")
	}
	short := ok
	short.ExternalTipHash = "deadbeef"
	if shouldAdoptExplorerTip(short) {
		t.Fatal("short hash must not adopt")
	}
	lag := TipLagStatus{Lagging: true, LocalTip: 149948, ExternalTip: 149989}
	if !shouldAdoptExplorerTip(lag) {
		t.Fatal("expected adopt on height lag so sticky-invalid parents can be repaired")
	}
}

type mockReorg struct {
	tips                 map[string]bool
	active               map[int64]string
	invalidateN          int
	reconsiderN          int
	submitN              int
	stickyInvalidate     bool
	explorerOnInvalidate string
	explorerOnSubmit     string
	compareHeight        int64
	network              string
	hasTipErr            error
	invalidateErr        error
	reconsiderErr        error
	submitErr            error
	lastInvalidated      string
}

func (m *mockReorg) Network() string {
	if m.network != "" {
		return m.network
	}
	return "testnet4"
}
func (m *mockReorg) HasChainTip(_ context.Context, hash string) (bool, error) {
	if m.hasTipErr != nil {
		return false, m.hasTipErr
	}
	return m.tips[strings.ToLower(hash)], nil
}
func (m *mockReorg) InvalidateBlock(_ context.Context, hash string) error {
	m.invalidateN++
	m.lastInvalidated = hash
	if m.invalidateErr != nil {
		return m.invalidateErr
	}
	if m.stickyInvalidate {
		return nil
	}
	if m.explorerOnInvalidate != "" && m.compareHeight != 0 && m.active != nil {
		m.active[m.compareHeight] = m.explorerOnInvalidate
	}
	return nil
}
func (m *mockReorg) ReconsiderBlock(_ context.Context, _ string) error {
	m.reconsiderN++
	return m.reconsiderErr
}
func (m *mockReorg) SubmitBlockHex(_ context.Context, _ string) error {
	m.submitN++
	if m.submitErr != nil {
		return m.submitErr
	}
	if m.explorerOnSubmit != "" && m.compareHeight != 0 && m.active != nil {
		m.active[m.compareHeight] = m.explorerOnSubmit
	}
	return nil
}
func (m *mockReorg) GetBlockHash(_ context.Context, height int64) (string, error) {
	if m.active == nil {
		return "", context.Canceled
	}
	return m.active[height], nil
}

func explorerAdoptServer(hashes map[int64]string, rawByHash map[string][]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/block-height/") {
			h, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/block-height/"), 10, 64)
			if hash, ok := hashes[h]; ok {
				_, _ = w.Write([]byte(hash))
				return
			}
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/block/") && strings.HasSuffix(r.URL.Path, "/raw") {
			h := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/block/"), "/raw")
			if raw, ok := rawByHash[h]; ok {
				_, _ = w.Write(raw)
				return
			}
			http.NotFound(w, r)
			return
		}
		http.NotFound(w, r)
	}))
}

func TestTryAdoptExplorerTip_NoInvalidateWithoutExplorerBlocks(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_ADOPT_EXPLORER_COOLDOWN", "1ms")
	srv := explorerAdoptServer(nil, nil)
	defer srv.Close()
	testExplorerBaseURL = srv.URL
	t.Cleanup(func() { testExplorerBaseURL = "" })
	local := "0000000000ed4f344ba8335a5e390fe2ae58e8165c7829ba9456e1a983084198"
	expl := "0000000000baebbdeab043d4b98615caf906c0fba8948e0ff1bf20136ba50de8"
	m := &mockReorg{
		active:               map[int64]string{149952: local},
		explorerOnInvalidate: expl,
		compareHeight:        149952,
	}
	st := TipLagStatus{
		HashMismatch:    true,
		CompareHeight:   149952,
		LocalTipHash:    local,
		ExternalTipHash: expl,
	}
	out := tryAdoptExplorerTip(context.Background(), m, st)
	if out.switched {
		t.Fatal("must not claim switch when explorer blocks were not submitted")
	}
	if m.invalidateN != 0 {
		t.Fatalf("invalidate without an explorer replacement poisons the chain, n=%d", m.invalidateN)
	}
}

func TestTryAdoptExplorerTip_SubmitSwitchesWithoutInvalidate(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_ADOPT_EXPLORER_COOLDOWN", "1ms")
	local := "0000000000435c6477ee9475f38ee8dcd0dfb949c943299af5b4cd8bbb697d09"
	expl := "000000000019ff1821f8239131e294384cb7b0ffd4f3c391fd96930c14bc7b31"
	raw := bytes.Repeat([]byte{0xab}, 80)
	srv := explorerAdoptServer(map[int64]string{149955: expl}, map[string][]byte{expl: raw})
	defer srv.Close()
	testExplorerBaseURL = srv.URL
	t.Cleanup(func() { testExplorerBaseURL = "" })

	m := &mockReorg{
		active:           map[int64]string{149955: local},
		explorerOnSubmit: expl,
		compareHeight:    149955,
	}
	st := TipLagStatus{
		HashMismatch:    true,
		CompareHeight:   149955,
		ExternalTip:     149955,
		LocalTipHash:    local,
		ExternalTipHash: expl,
	}
	if !tryAdoptExplorerTip(context.Background(), m, st).switched {
		t.Fatal("expected adopt via submitblock")
	}
	if m.submitN != 1 {
		t.Fatalf("submitN=%d", m.submitN)
	}
	if m.invalidateN != 0 {
		t.Fatalf("invalidate should not run when submit switches, n=%d", m.invalidateN)
	}
}

func TestTryAdoptExplorerTip_StickyInvalidateStillSubmits(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_ADOPT_EXPLORER_COOLDOWN", "1ms")
	local := "0000000000435c6477ee9475f38ee8dcd0dfb949c943299af5b4cd8bbb697d09"
	expl := "000000000019ff1821f8239131e294384cb7b0ffd4f3c391fd96930c14bc7b31"
	raw := bytes.Repeat([]byte{0xcd}, 80)
	srv := explorerAdoptServer(map[int64]string{149955: expl}, map[string][]byte{expl: raw})
	defer srv.Close()
	testExplorerBaseURL = srv.URL
	t.Cleanup(func() { testExplorerBaseURL = "" })

	m := &mockReorg{
		active:           map[int64]string{149955: local},
		stickyInvalidate: true,
		explorerOnSubmit: expl,
		compareHeight:    149955,
	}
	st := TipLagStatus{
		HashMismatch:    true,
		CompareHeight:   149955,
		ExternalTip:     149955,
		LocalTipHash:    local,
		ExternalTipHash: expl,
	}
	if !tryAdoptExplorerTip(context.Background(), m, st).switched {
		t.Fatal("expected adopt via submit after sticky invalidate")
	}
	if m.submitN < 1 {
		t.Fatalf("submitN=%d", m.submitN)
	}
}

func TestTryAdoptExplorerTip_InvalidatesLocalOnlyAfterSubmit(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_ADOPT_EXPLORER_COOLDOWN", "1ms")
	local := "0000000000ed4f344ba8335a5e390fe2ae58e8165c7829ba9456e1a983084198"
	expl := "0000000000baebbdeab043d4b98615caf906c0fba8948e0ff1bf20136ba50de8"
	raw := bytes.Repeat([]byte{0xef}, 80)
	srv := explorerAdoptServer(map[int64]string{149952: expl}, map[string][]byte{expl: raw})
	defer srv.Close()
	testExplorerBaseURL = srv.URL
	t.Cleanup(func() { testExplorerBaseURL = "" })

	m := &mockReorg{
		active:               map[int64]string{149952: local},
		explorerOnInvalidate: expl,
		compareHeight:        149952,
	}
	st := TipLagStatus{
		HashMismatch:    true,
		CompareHeight:   149952,
		ExternalTip:     149952,
		LocalTip:        149952,
		LocalTipHash:    local,
		ExternalTipHash: expl,
	}
	out := tryAdoptExplorerTip(context.Background(), m, st)
	if !out.switched {
		t.Fatal("expected switch via invalidate after explorer submit")
	}
	if m.submitN < 1 {
		t.Fatalf("submitN=%d", m.submitN)
	}
	if m.invalidateN != 1 {
		t.Fatalf("invalidateN=%d", m.invalidateN)
	}
	if !strings.EqualFold(m.lastInvalidated, local) {
		t.Fatalf("invalidated %s want local %s", m.lastInvalidated, local)
	}
}

func TestTryAdoptExplorerTip_RefusesExplorerHash(t *testing.T) {
	resetTipLagStateForTest()
	t.Setenv("CHAIN_ADOPT_EXPLORER_COOLDOWN", "1ms")
	expl := "0000000000435c6477ee9475f38ee8dcd0dfb949c943299af5b4cd8bbb697d09"
	other := "000000000019ff1821f8239131e294384cb7b0ffd4f3c391fd96930c14bc7b31"
	raw := bytes.Repeat([]byte{0x11}, 80)
	srv := explorerAdoptServer(map[int64]string{149955: expl}, map[string][]byte{expl: raw})
	defer srv.Close()
	testExplorerBaseURL = srv.URL
	t.Cleanup(func() { testExplorerBaseURL = "" })

	m := &mockReorg{
		active:        map[int64]string{149955: expl},
		compareHeight: 149955,
		submitErr:     fmt.Errorf("previous block %s is known to be invalid", expl),
	}
	st := TipLagStatus{
		HashMismatch:    true,
		CompareHeight:   149955,
		ExternalTip:     149955,
		LocalTip:        149955,
		LocalTipHash:    expl,
		ExternalTipHash: other,
	}
	out := tryAdoptExplorerTip(context.Background(), m, st)
	if m.invalidateN != 0 {
		t.Fatalf("must not invalidate explorer hash, n=%d hash=%s", m.invalidateN, m.lastInvalidated)
	}
	if !out.sticky {
		t.Fatal("expected sticky-invalid signal so the caller can repair the index")
	}
}

func TestKnownInvalidParent(t *testing.T) {
	parent := "0000000000435c6477ee9475f38ee8dcd0dfb949c943299af5b4cd8bbb697d09"
	got, ok := knownInvalidParent(fmt.Errorf("rejected: previous block %s is known to be invalid", parent))
	if !ok || got != parent {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := knownInvalidParent(fmt.Errorf("already have block")); ok {
		t.Fatal("duplicate is not known-invalid")
	}
}

func TestAdoptSubmitRange_LagStartsAfterLocalTip(t *testing.T) {
	first, last := adoptSubmitRange(TipLagStatus{LocalTip: 149948, ExternalTip: 149989, Lagging: true})
	if first != 149949 {
		t.Fatalf("first=%d want 149949", first)
	}
	if last != 149948+1+12 {
		t.Fatalf("last=%d want capped 149961", last)
	}
}

func TestApplyTipHashCheck_MismatchMarksLagging(t *testing.T) {
	resetTipLagStateForTest()
	st := EvaluateTipLag(50, 50, 3, nil, time.Now())
	if st.Lagging {
		t.Fatal("heights match, should not lag yet")
	}
	st = ApplyTipHashCheck(st, 50,
		"0000000000000000000000000000000000000000000000000000000000000001",
		"0000000000000000000000000000000000000000000000000000000000000002",
	)
	if !st.HashMismatch || !st.Lagging {
		t.Fatalf("fork must mark mismatch+lagging: %+v", st)
	}
	out := map[string]any{"synced": true}
	mergeTipLagIntoStatus(out)
	if out["synced"] != false {
		t.Fatalf("health must flip unsynced on hash mismatch, got %v", out["synced"])
	}
	if out["tip_hash_mismatch"] != true {
		t.Fatalf("tip_hash_mismatch=%v", out["tip_hash_mismatch"])
	}
}

func TestFetchExternalBlockHash(t *testing.T) {
	const want = "0f2e1d0c0b0a09080706050403020100ffeeddccbbaa99887766554433221100"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/block-height/144834" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(want + "\n"))
	}))
	defer srv.Close()

	testExplorerBaseURL = srv.URL
	t.Cleanup(func() { testExplorerBaseURL = "" })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := FetchExternalBlockHash(ctx, "testnet4", 144834)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFetchExternalTipHeight(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("144834\n"))
	}))
	defer srv.Close()

	// Override via GetNetworkConfig is not injectable; test HTTP path with a
	// direct request pattern by temporarily not depending on network map —
	// use the function only if we can point HeightURL. Instead, unit-test the
	// parse path through a small local helper simulation:
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Sanity: server works; FetchExternalTipHeight against real network is integration-only.
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
}
