package ed2k

import "encoding/hex"

func hexEncode(b []byte) string { return hex.EncodeToString(b) }

func hexDecodeHash(s string) ([]byte, error) { return hex.DecodeString(s) }
