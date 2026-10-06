package smart_contract

import (
	"fmt"
	"strings"

	"stargate-backend/bitcoin"
	"stargate-backend/stego"
)

// VerifyCreatorAttestation checks that sig is a Bitcoin signed message from
// wallet over stego.WishCreatorMessage(wishHash). Missing or invalid input is
// a verification failure, not a reason to trust the claimed wallet.
func VerifyCreatorAttestation(wallet, sig, wishHash string) error {
	wallet = strings.TrimSpace(wallet)
	sig = strings.TrimSpace(sig)
	wishHash = strings.TrimSpace(wishHash)
	if wallet == "" || sig == "" || wishHash == "" {
		return fmt.Errorf("creator attestation incomplete")
	}
	ok, err := bitcoin.VerifyBTCSignature(wallet, sig, stego.WishCreatorMessage(wishHash))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("creator attestation signature invalid")
	}
	return nil
}
