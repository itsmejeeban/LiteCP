package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "strings"
 "time"
)
func TestPHPMyAdminRequiresPanelAuthentication(t *testing.T) {
 app:=&App{}
 for _,method:=range []string{http.MethodGet,http.MethodPost}{
  response:=httptest.NewRecorder()
  app.routes().ServeHTTP(response,httptest.NewRequest(method,"/api/phpmyadmin/index.php",nil))
  if response.Code!=http.StatusUnauthorized{t.Fatalf("%s phpMyAdmin returned %d",method,response.Code)}
 }
}

func TestPHPMyAdminTicketOneUseAndSessionBinding(t *testing.T){
 store,err:=OpenStore(t.TempDir());if err!=nil{t.Fatal(err)}
 if err=store.AddDatabase(ManagedDatabase{ID:"db",Name:"example_db",Username:"example_user"});err!=nil{t.Fatal(err)}
 app:=&App{store:store,sessions:map[string]session{"active":{Expires:time.Now().Add(time.Hour)}}}
 launch:=pmaLaunch{DatabaseID:"db",Database:"example_db",Username:"lpma_test",Password:"private",Session:"active",Expires:time.Now().Add(time.Minute)}
 pmaTicketMu.Lock();pmaTickets["once"]=launch;pmaTicketMu.Unlock()
 consume:=func(forwarded bool)int{request:=httptest.NewRequest("POST","http://127.0.0.1:9090/internal/phpmyadmin-ticket",strings.NewReader(`{"ticket":"once"}`));request.RemoteAddr="127.0.0.1:12345";if forwarded{request.Header.Set("X-Forwarded-For","203.0.113.1")};response:=httptest.NewRecorder();app.consumePMATicket(response,request);return response.Code}
 if consume(true)!=403{t.Fatal("public proxy request reached ticket redemption")}
 if consume(false)!=200{t.Fatal("valid local ticket rejected")}
 if consume(false)!=403{t.Fatal("ticket could be replayed")}
 launch.Session="expired";pmaTicketMu.Lock();pmaTickets["once"]=launch;pmaTicketMu.Unlock()
 if consume(false)!=403{t.Fatal("expired panel session accepted")}
 launch.Session="active";launch.Expires=time.Now().Add(-time.Second);pmaTicketMu.Lock();pmaTickets["once"]=launch;pmaTicketMu.Unlock()
 if consume(false)!=403{t.Fatal("expired launch ticket accepted")}
}
