package smart_contract

import (
	"errors"
	"strings"
	"testing"
	"time"

	"stargate-backend/core/smart_contract"
)

func TestDecideClaimIdempotentSameWallet(t *testing.T) {
	now := time.Now()
	existing := smart_contract.Claim{
		ClaimID: "CLAIM-1", TaskID: "t1", AiIdentifier: "WalletA",
		Status: "active", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	plan, err := DecideClaim(ClaimInput{
		TaskID: "t1", TaskStatus: "claimed", Wallet: "walleta",
		ExistingClaims: []smart_contract.Claim{existing},
		Now: now, ClaimTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != ClaimActionIdempotent || plan.Claim.ClaimID != "CLAIM-1" {
		t.Fatalf("plan: %+v", plan)
	}
}

func TestDecideClaimTakenByOther(t *testing.T) {
	now := time.Now()
	existing := smart_contract.Claim{
		ClaimID: "CLAIM-1", TaskID: "t1", AiIdentifier: "other",
		Status: "active", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}
	_, err := DecideClaim(ClaimInput{
		TaskID: "t1", TaskStatus: "claimed", Wallet: "me",
		ExistingClaims: []smart_contract.Claim{existing},
		Now: now,
	})
	if !errors.Is(err, ErrTaskTaken) {
		t.Fatalf("want ErrTaskTaken, got %v", err)
	}
}

func TestDecideClaimBlocksTerminalStatuses(t *testing.T) {
	now := time.Now()
	for _, st := range []string{"submitted", "approved", "published", "completed", "pending_review"} {
		_, err := DecideClaim(ClaimInput{
			TaskID: "t1", TaskStatus: st, Wallet: "me", Now: now, ClaimTTL: time.Hour, NewClaimID: "CLAIM-x",
		})
		if !errors.Is(err, ErrTaskUnavailable) {
			t.Fatalf("status %s: want unavailable, got %v", st, err)
		}
	}
}

func TestDecideClaimAllowsStaleClaimedStatus(t *testing.T) {
	// Task still "claimed" but no active claim (expired) — new claimer OK.
	now := time.Now()
	expired := smart_contract.Claim{
		ClaimID: "CLAIM-old", TaskID: "t1", AiIdentifier: "old",
		Status: "active", ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-2 * time.Hour),
	}
	plan, err := DecideClaim(ClaimInput{
		TaskID: "t1", TaskStatus: "claimed", Wallet: "new",
		ExistingClaims: []smart_contract.Claim{expired},
		Now: now, ClaimTTL: time.Hour, NewClaimID: "CLAIM-new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != ClaimActionCreate || plan.Claim.ClaimID != "CLAIM-new" {
		t.Fatalf("plan: %+v", plan)
	}
}

func TestDecideClaimRequiresWallet(t *testing.T) {
	_, err := DecideClaim(ClaimInput{TaskID: "t1", TaskStatus: "available", Wallet: "  "})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecideClaimCreatesAndSetsProof(t *testing.T) {
	now := time.Now()
	plan, err := DecideClaim(ClaimInput{
		TaskID: "t1", TaskStatus: "available", Wallet: "bc1qtest",
		Now: now, ClaimTTL: 2 * time.Hour, NewClaimID: "CLAIM-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != ClaimActionCreate {
		t.Fatal(plan.Action)
	}
	if !plan.UpdateProof || plan.Proof == nil || plan.Proof.ContractorWallet != "bc1qtest" {
		t.Fatalf("proof: %+v update=%v", plan.Proof, plan.UpdateProof)
	}
	if plan.Claim.Status != smart_contract.ClaimStatusActive {
		t.Fatal(plan.Claim.Status)
	}
}

func TestDecideSubmitResubmitAndExpire(t *testing.T) {
	now := time.Now()
	claim := smart_contract.Claim{
		ClaimID: "c1", TaskID: "t1", Status: "submitted",
		ExpiresAt: now.Add(time.Hour),
	}
	_, err := DecideSubmit(SubmitInput{
		Claim: claim, ExistingSubmissionStatuses: []string{"pending_review"}, Now: now,
	})
	if err == nil {
		t.Fatal("expected no resubmit")
	}
	plan, err := DecideSubmit(SubmitInput{
		Claim: claim, ExistingSubmissionStatuses: []string{"rejected"},
		Deliverables: map[string]interface{}{"status": "hack", "a": 1},
		Now: now, NewSubmissionID: "SUB-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ReactivateClaim || plan.Submission.SubmissionID != "SUB-1" {
		t.Fatalf("%+v", plan)
	}
	if _, ok := plan.Submission.Deliverables["status"]; ok {
		t.Fatal("status key should be stripped")
	}

	expired := claim
	expired.Status = "active"
	expired.ExpiresAt = now.Add(-time.Second)
	plan2, err := DecideSubmit(SubmitInput{Claim: expired, Now: now})
	if err == nil || !plan2.MarkClaimExpired {
		t.Fatalf("expire: plan=%+v err=%v", plan2, err)
	}
}

func TestDecideSubmissionStatusUpdate(t *testing.T) {
	now := time.Now()
	p := DecideSubmissionStatusUpdate("approved", "n", "t", now)
	if p.Cascade != SubmissionCascadeApprove || p.TaskStatus != "approved" {
		t.Fatalf("%+v", p)
	}
	p = DecideSubmissionStatusUpdate("rejected", "bad", "quality", now)
	if p.Cascade != SubmissionCascadeReject || p.RejectedAt == nil || p.RejectionReason != "bad" {
		t.Fatalf("%+v", p)
	}
	p = DecideSubmissionStatusUpdate("reviewed", "x", "y", now)
	if p.Cascade != SubmissionCascadeNone || p.RejectionReason != "" {
		t.Fatalf("%+v", p)
	}
}

func TestCheckProposalApprovableAndPublishable(t *testing.T) {
	if err := CheckProposalApprovable("p1", "pending"); err != nil {
		t.Fatal(err)
	}
	if err := CheckProposalApprovable("p1", "approved"); err == nil {
		t.Fatal("expected already approved")
	}
	if err := CheckProposalPublishable("p1", "approved"); err != nil {
		t.Fatal(err)
	}
	if err := CheckProposalPublishable("p1", "pending"); err == nil {
		t.Fatal("expected must be approved")
	}
}

func TestContractStatusMaySupersede(t *testing.T) {
	if ContractStatusMaySupersede("confirmed") || ContractStatusMaySupersede("completed") {
		t.Fatal("must not supersede confirmed/completed")
	}
	if !ContractStatusMaySupersede("active") {
		t.Fatal("active should supersede")
	}
}

func TestBuildApprovePlanAndPublish(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	p := &smart_contract.Proposal{
		ID: "p1", Status: "pending",
		VisiblePixelHash: hash,
		Tasks:            []smart_contract.Task{{TaskID: "t1", BudgetSats: 1000, Title: "t"}},
		Metadata:         map[string]interface{}{"contract_id": "abc", "visible_pixel_hash": hash},
	}
	plan, err := BuildApprovePlan("p1", "pending", p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plan.NewProposalStatus != "approved" || plan.Keys.NormalizedContractID == "" {
		t.Fatalf("%+v", plan)
	}
	_, err = BuildApprovePlan("p1", "pending", &smart_contract.Proposal{
		ID: "p1", Status: "pending", VisiblePixelHash: hash,
		Metadata: map[string]interface{}{"visible_pixel_hash": hash},
	}, 0)
	if err == nil {
		t.Fatal("expected no tasks error")
	}
	pub, err := BuildPublishPlan("p1", "approved", map[string]interface{}{"contract_id": "c1"})
	if err != nil || pub.ContractID != "c1" {
		t.Fatalf("%+v %v", pub, err)
	}
}

func TestBuildConfirmApply(t *testing.T) {
	// 64-hex pixel hash
	hash := strings.Repeat("ab", 32)
	apply := BuildConfirmApply(hash, 100, "")
	if !apply.Plan.IsPixelHash {
		t.Fatal("expected pixel hash")
	}
	if apply.StegoImageURL == "" || !strings.Contains(apply.StegoImageURL, hash) {
		t.Fatalf("url: %s", apply.StegoImageURL)
	}
	meta := MergeConfirmMetadata(nil, "txid1", 100)
	if meta["confirmed_txid"] != "txid1" {
		t.Fatal(meta)
	}
}
