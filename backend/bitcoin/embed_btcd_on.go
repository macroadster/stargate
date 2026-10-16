//go:build embedbtcd

package bitcoin

import _ "embed"

//go:embed btcd.embed
var embeddedBtcd []byte
