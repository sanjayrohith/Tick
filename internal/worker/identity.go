package worker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

// Identity returns a stable worker identity for claimed_by:
// "hostname/pid/short-uuid". Logging it once at startup is what lets an
// orphaned row found later be traced back to the real process that claimed
// it, rather than to a bare number nobody can place.
func Identity() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s/%d/%s", host, os.Getpid(), shortID())
}

// shortID returns eight hex characters of randomness -- enough to tell apart
// two processes that share a hostname and, within one restart cycle, a pid.
func shortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}
