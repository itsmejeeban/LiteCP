package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
)
func TestPHPMyAdminRequiresPanelAuthentication(t *testing.T) {
 app:=&App{}
 for _,method:=range []string{http.MethodGet,http.MethodPost}{
  response:=httptest.NewRecorder()
  app.routes().ServeHTTP(response,httptest.NewRequest(method,"/api/phpmyadmin/index.php",nil))
  if response.Code!=http.StatusUnauthorized{t.Fatalf("%s phpMyAdmin returned %d",method,response.Code)}
 }
}
