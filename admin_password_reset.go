package main

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "time"
)

// resetAdministratorPassword changes only the password hash. Authenticator
// configuration, consumed recovery codes and the administrator name stay intact.
func resetAdministratorPassword(store *Store, password string) error {
 if !store.Initialized() {return errors.New("administrator is not initialized")}
 if len(password)<12 || len(password)>1024 {return errors.New("use a password between 12 and 1024 bytes")}
 return store.SetPassword(password)
}

func resetAdminPasswordCommand(dataDir string) (result error) {
 if runtime.GOOS!="linux" || os.Geteuid()!=0 {return errors.New("reset-admin-password requires Linux root access")}
 // Do not create a new state directory when a path was mistyped.
 if _,err:=os.Stat(filepath.Join(dataDir,"config.json"));err!=nil {return fmt.Errorf("cannot locate LiteCP state in %s: %w",dataDir,err)}
 prompt:=exec.Command("bash","-c",`set -e
exec 3<>/dev/tty
printf 'New administrator password (12-1024 characters): ' >&3
IFS= read -r -s first <&3
printf '\nConfirm new password: ' >&3
IFS= read -r -s second <&3
printf '\n' >&3
if [ "$first" != "$second" ]; then printf 'Passwords do not match.\n' >&3; exit 1; fi
printf '%s' "$first"`)
 prompt.Stderr=os.Stderr
 raw,err:=prompt.Output();if err!=nil {return errors.New("password entry cancelled, unavailable, or confirmation did not match; no password changed")}
 defer func(){for i:=range raw {raw[i]=0}}()
 if len(raw)<12 || len(raw)>1024 {return errors.New("use a password between 12 and 1024 bytes; no password changed")}
 command:=func(args ...string) error {ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel();return exec.CommandContext(ctx,"systemctl",args...).Run()}
 // Refuse to update a stopped service: requiring the installed service to be
 // running makes the reset/restart lifecycle explicit and avoids offline copies.
 if err:=command("is-active","--quiet","litecp.service");err!=nil {return errors.New("start litecp.service before resetting its administrator password")}
 fmt.Println("Stopping LiteCP briefly to safely update authentication state...")
 if err:=command("stop","litecp.service");err!=nil {return errors.New("could not stop LiteCP; no password changed")}
 defer func(){if err:=command("start","litecp.service");err!=nil {result=fmt.Errorf("%v; LiteCP did not restart: run systemctl start litecp.service and inspect its logs",result)}}()
 store,err:=OpenStore(dataDir);if err!=nil {return err}
 if err:=resetAdministratorPassword(store,string(raw));err!=nil {return err}
 if err:=store.AddActivity("Administrator password reset","Password reset through the root terminal; 2FA preserved.","warning");err!=nil {fmt.Fprintln(os.Stderr,"Password changed, but the activity event could not be saved.")}
 fmt.Println("Administrator password reset. Existing panel sessions are ended. Sign in with the new password.")
 if store.twoFactor().Enabled {fmt.Println("2FA remains enabled: an authenticator code or unused recovery code is still required.")}
 return nil
}

// Keep command arguments free of passwords and reject accidental extra input.
func validateResetPasswordArguments(args []string) error {
 if len(args)!=2 || strings.TrimSpace(args[1])!="reset-admin-password" {return errors.New("usage: litecp reset-admin-password (no password arguments)")}
 return nil
}
