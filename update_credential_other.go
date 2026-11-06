//go:build !linux

package main

import "os"

// Private server updates are supported only on Linux, with checked ownership.
func updateCredentialRootOwned(_ os.FileInfo) bool { return false }
