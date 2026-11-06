package main

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestArgon2PasswordRandomSaltAndBoundedParameters(t *testing.T) {
	first, err := hashPassword("correct-test-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword("correct-test-password")
	if err != nil || first == second {
		t.Fatal("salt reuse", err)
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$m=19456,t=2,p=1$") || !verifyPassword(first, "correct-test-password") || verifyPassword(first, "wrong") {
		t.Fatal("Argon2 roundtrip failed")
	}
	for _, bad := range []string{strings.Replace(first, "m=19456", "m=4294967295", 1), strings.Replace(first, "t=2", "t=0", 1), strings.Replace(first, "p=1", "p=255", 1), strings.Replace(first, "v=19", "v=16", 1), first + "$extra", strings.Replace(first, "m=19456,t=2,p=1", "m=19456,t=2,p=1junk", 1)} {
		if verifyPassword(bad, "correct-test-password") {
			t.Fatal("malformed/unbounded hash accepted")
		}
	}
}

func TestLegacyPasswordUpgradePreservesAuthentication(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("legacy-test-salt")
	key := pbkdf2SHA256([]byte("legacy-test-password"), salt, 100000, 32)
	legacy := fmt.Sprintf("pbkdf2-sha256$100000$%s$%s", hex.EncodeToString(salt), hex.EncodeToString(key))
	store.cfg.Admin = Admin{Username: "admin", PasswordHash: legacy}
	if store.CheckPassword("unknown", "legacy-test-password") || store.cfg.Admin.PasswordHash != legacy {
		t.Fatal("unknown username changed hash")
	}
	if store.CheckPassword("admin", "wrong") || store.cfg.Admin.PasswordHash != legacy {
		t.Fatal("invalid password changed hash")
	}
	if !store.CheckPassword("admin", "legacy-test-password") || !strings.HasPrefix(store.cfg.Admin.PasswordHash, "$argon2id$") {
		t.Fatal("migration failed")
	}
	reopened, err := OpenStore(filepath.Dir(store.path))
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.CheckPassword("admin", "legacy-test-password") {
		t.Fatal("migrated password not persisted")
	}
}
