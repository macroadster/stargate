//go:build !embedbtcd

package bitcoin

// embeddedBtcd is empty unless the binary is built with -tags embedbtcd
// after scripts/build-btcd.sh writes bitcoin/btcd.embed.
var embeddedBtcd []byte
