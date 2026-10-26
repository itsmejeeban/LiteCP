package main

import (
 "net/http"
 "net/http/httputil"
 "net/url"
 "strings"
)

// phpMyAdmin is reachable only through an authenticated panel session. Its
// own cookie authentication uses the restricted database user's password.
func (a *App) phpMyAdmin(w http.ResponseWriter, r *http.Request) {
 if a.system.demo { writeError(w,503,"phpMyAdmin requires the installed server."); return }
 if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost { writeError(w,405,"Method not allowed."); return }
 target,_:=url.Parse("http://127.0.0.1:9091")
 proxy:=httputil.NewSingleHostReverseProxy(target)
 original:=proxy.Director
 proxy.Director=func(request *http.Request){
  original(request)
  request.URL.Path="/"+strings.TrimPrefix(r.URL.Path,"/api/phpmyadmin/")
  request.URL.RawPath=""
  request.Header.Set("X-Forwarded-Host",r.Host)
  scheme:="http";if requestIsHTTPS(r){scheme="https"}
  request.Header.Set("X-Forwarded-Proto",scheme)
  // Never pass the panel session credential into PHP.
  request.Header.Del("Cookie")
  for _,cookie:=range r.Cookies(){name:=strings.TrimPrefix(cookie.Name,"__Secure-");if strings.HasPrefix(name,"pma")||name=="phpMyAdmin"||name=="phpMyAdmin_https"||name=="LiteCPSignon"{request.AddCookie(cookie)}}
 }
 proxy.ModifyResponse=func(response *http.Response)error{
  // phpMyAdmin supplies the CSP required by its bundled scripts.
  if response.Header.Get("Content-Security-Policy")=="" { response.Header.Set("Content-Security-Policy","default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self'") }
  response.Header.Set("X-Frame-Options","DENY")
  response.Header.Set("Cache-Control","no-store")
  return nil
 }
 proxy.ErrorHandler=func(w http.ResponseWriter,r *http.Request,err error){writeError(w,503,"phpMyAdmin is unavailable. Run scripts/install-phpmyadmin.sh on the server.")}
 w.Header().Del("Content-Security-Policy")
 proxy.ServeHTTP(w,r)
}
