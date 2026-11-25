package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// Connect-only Linux windows cannot route a relaunch to a managed window,
// where the launch would otherwise start the service as a side effect.
func windowIdentity(socket string, managedLinux bool) string {
	digest := sha256.Sum256([]byte(filepath.Clean(socket)))
	identity := "org.panora.client." + hex.EncodeToString(digest[:16])
	if managedLinux {
		identity += ".managed"
	}
	return identity
}
