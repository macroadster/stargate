package handlers

import (
	"strings"
	"testing"

	sc "stargate-backend/core/smart_contract"
)

func TestContractToInscriptionRequestWishAlias(t *testing.T) {
	hash := "2d89e6ebfd09604938416481cb69cee2a63ceadab93541b3fbf4a26293274bf7"
	got := contractToInscriptionRequest(sc.Contract{
		ContractID:      hash,
		Title:           "Loops",
		Status:          "pending",
		TotalBudgetSats: 1000,
	})
	if got.ID != "wish-"+hash {
		t.Fatalf("id %q", got.ID)
	}
	if got.VisiblePixelHash != hash {
		t.Fatalf("visible %q", got.VisiblePixelHash)
	}
	if strings.HasPrefix(got.ImageData, "/uploads/wish-") {
		t.Fatalf("image path kept the prefix: %s", got.ImageData)
	}

	already := contractToInscriptionRequest(sc.Contract{
		ContractID: "wish-" + hash,
		Status:     "pending",
	})
	if already.ID != "wish-"+hash {
		t.Fatalf("double prefix: %q", already.ID)
	}

	other := contractToInscriptionRequest(sc.Contract{
		ContractID: "proposal-not-a-pixel",
		Status:     "pending",
	})
	if other.ID != "proposal-not-a-pixel" {
		t.Fatalf("non-pixel id changed: %q", other.ID)
	}
}
