package main

import (
 "bytes"
 "os"
 "path/filepath"
 "reflect"
 "testing"
)

func TestRootPasswordResetPreservesTwoFactor(t *testing.T) {
 app:=twoFactorTestApp(t)
 if err:=app.store.updateTwoFactor(func(v *twoFactorConfig) error {
  v.Enabled=true;v.SecretCipher="encrypted-seed";v.RecoveryHashes=[]string{recoveryHash("unused-code")};v.LastStep=123;v.Revision="existing-revision";return nil
 });err!=nil {t.Fatal(err)}
 before:=app.store.twoFactor()
 if err:=resetAdministratorPassword(app.store,"new-password-long-enough");err!=nil {t.Fatal(err)}
 reopened,err:=OpenStore(filepath.Dir(app.store.path));if err!=nil {t.Fatal(err)}
 if !reflect.DeepEqual(before,reopened.twoFactor()) {t.Fatal("password reset modified 2FA or recovery codes")}
 if !reopened.CheckPassword("admin","new-password-long-enough") || reopened.CheckPassword("admin","test-password-long-enough") {t.Fatal("password reset failed")}
 disk,err:=os.ReadFile(app.store.path);if err!=nil {t.Fatal(err)}
 if bytes.Contains(disk,[]byte("new-password-long-enough")) {t.Fatal("plaintext password persisted")}
 if err:=resetAdministratorPassword(app.store,"short");err==nil {t.Fatal("short password accepted")}
 if err:=validateResetPasswordArguments([]string{"litecp","reset-admin-password","secret"});err==nil {t.Fatal("password argument accepted")}
}
