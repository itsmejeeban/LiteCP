package main

import (
 "os"
 "os/exec"
 "strings"
 "testing"
)

func TestInstallerGeneratesFreshAdminPasswords(t *testing.T) {
 bash, err := exec.LookPath("bash"); if err != nil { t.Skip("bash required") }
 data, err := os.ReadFile("scripts/install.sh"); if err != nil { t.Fatal(err) }
 script := strings.ReplaceAll(string(data), "\r\n", "\n")
 start := strings.Index(script, "  ADMIN_PASSWORD=\"$(openssl rand -hex 24)\"")
 if start < 0 { t.Fatal("secure password generation missing") }
 end := strings.Index(script[start:], "  LITECP_DATA_DIR=")
 if end < 0 { t.Fatal("initialization missing") }
 generate := script[start:start+end]
 seen := map[string]bool{}
 for i:=0;i<3;i++ {
  cmd:=exec.Command(bash,"-eu","-c","fail() { exit 1; }; "+generate+"printf '%s' \"$ADMIN_PASSWORD\"")
  cmd.Env=append(os.Environ(),"LITECP_ADMIN_PASSWORD=reused-password-must-not-win")
  value,err:=cmd.Output(); if err!=nil {t.Fatal(err)}
  if len(value)!=48 || seen[string(value)] {t.Fatal("password length or uniqueness failed")};seen[string(value)]=true
 }
 if !strings.Contains(script,"chmod 0600 /var/lib/litecp/initial-admin-credentials.txt") {t.Fatal("credential file permissions missing")}
}
