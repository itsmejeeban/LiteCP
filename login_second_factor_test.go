package main
import("bytes";"encoding/json";"net/http/httptest";"strings";"testing")
func TestLoginSecondFactorPromptOnlyAfterCorrectPassword(t *testing.T){
 app:=twoFactorTestApp(t);app.store.cfg.Admin.TwoFactor.Enabled=true
 for _,password:=range []string{"wrong-password","test-password-long-enough"}{
  body,_:=json.Marshal(map[string]string{"username":"admin","password":password});r:=httptest.NewRequest("POST","https://panel.test/api/login",bytes.NewReader(body));r.RemoteAddr="192.0.2.10:1234";w:=httptest.NewRecorder();app.login(w,r)
  required:=strings.Contains(w.Body.String(),`"twoFactorRequired":true`)
  if required!=(password=="test-password-long-enough"){t.Fatalf("unexpected challenge: %s",w.Body.String())}
  if w.Code!=401||len(w.Result().Cookies())!=0{t.Fatal("challenge created a session")}
 }
}
