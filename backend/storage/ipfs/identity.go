package ipfs

import (
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"stargate-backend/storage/datadir"

	"github.com/libp2p/go-libp2p/core/crypto"
)

const defaultIdentityFile = "ipfs_identity.key"

// IdentityPath returns the on-disk libp2p private-key path.
// Override with IPFS_IDENTITY_FILE; otherwise $STARGATE_DATA_DIR/ipfs_identity.key.
func IdentityPath() string {
	if p := strings.TrimSpace(os.Getenv("IPFS_IDENTITY_FILE")); p != "" {
		return p
	}
	return datadir.Path(defaultIdentityFile)
}

// LoadOrCreateIdentity loads a protobuf-encoded libp2p private key from path,
// or generates an Ed25519 key and writes it (mode 0600) if the file is missing.
func LoadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("ipfs identity path is empty")
	}
	if data, err := os.ReadFile(path); err == nil {
		priv, err := crypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("decode ipfs identity %s: %w", path, err)
		}
		return priv, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read ipfs identity %s: %w", path, err)
	}

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ipfs identity: %w", err)
	}
	raw, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal ipfs identity: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir ipfs identity dir: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, fmt.Errorf("write ipfs identity %s: %w", path, err)
	}
	log.Printf("IPFS identity created at %s", path)
	return priv, nil
}
