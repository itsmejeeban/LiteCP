package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
 "context"
 "fmt"
 "os/user"
 "runtime"
)

func TestReconcileApplicationRoutingWithoutFastCGISocket(t *testing.T){
 system:=&System{}
 for _,enabled:=range []bool{false,true}{
  path:=filepath.Join(t.TempDir(),"application.conf")
  original:="server {\n listen 443 ssl;\n ssl_certificate /live/current/cert.pem;\n ssl_certificate_key /live/current/key.pem;\n location / { return 503; }\n}\n"
  if err:=os.WriteFile(path,[]byte(original),0600);err!=nil{t.Fatal(err)}
  app:=&ApplicationHosting{Enabled:enabled,Port:21000,Certificate:"/old/cert.pem",CertificateKey:"/old/key.pem"};site:=Site{Domain:"application.example.test",Root:"/var/www/application/public",SSL:true,Application:app}
  if err:=system.reconcileSiteNginx(site,path);err!=nil{t.Fatalf("enabled=%t: %v",enabled,err)}
  content,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)};config:=string(content)
  if strings.Contains(config,"fastcgi_pass")||!strings.Contains(config,"/live/current/cert.pem"){t.Fatal("application reconciliation used PHP or lost live TLS")}
  if enabled&&!strings.Contains(config,"proxy_pass http://127.0.0.1:21000"){t.Fatal("proxy routing lost")}
  if !enabled&&(!strings.Contains(config,"return 503")||!strings.Contains(config,"X-LiteCP-Application-State maintenance")){t.Fatal("maintenance routing lost")}
  if app.Certificate!="/old/cert.pem"{t.Fatal("reconciliation changed stored object in place")}
 }
}
func TestReconcilePHPStillRejectsMissingFastCGISocket(t *testing.T){
 path:=filepath.Join(t.TempDir(),"php.conf");original:="server { location / { return 503; } }";if err:=os.WriteFile(path,[]byte(original),0600);err!=nil{t.Fatal(err)}
 if err:=(&System{}).reconcileSiteNginx(Site{Domain:"php.example.test",PHPVersion:"8.3"},path);err==nil{t.Fatal("invalid ordinary PHP config accepted")};content,_:=os.ReadFile(path);if string(content)!=original{t.Fatal("invalid PHP config overwritten")}
}

func TestReconcileApplicationPipelineSkipsPHPForEveryRuntime(t *testing.T){
 if runtime.GOOS!="linux"||os.Geteuid()!=0{t.Skip("isolated ownership fixture needs Linux root")};if _,err:=user.Lookup("nginx");err!=nil{t.Skip("nginx account unavailable")}
 for _,language:=range []string{"node","python","ruby"}{for _,enabled:=range []bool{false,true}{t.Run(fmt.Sprintf("%s-enabled=%t",language,enabled),func(t *testing.T){
  root:=t.TempDir();public:=filepath.Join(root,"website","public");if err:=os.MkdirAll(public,0750);err!=nil{t.Fatal(err)};path:=filepath.Join(root,"site.conf")
  original:="server {\n ssl_certificate /current/cert.pem;\n ssl_certificate_key /current/key.pem;\n location / { return 503; }\n}\n";if err:=os.WriteFile(path,[]byte(original),0600);err!=nil{t.Fatal(err)}
  // UID/GID zero and an invalid PHP version intentionally fail PHP preparation.
  // A configured application must skip that path entirely, even when disabled.
  site:=Site{Domain:"fixture.example.test",Root:public,PHPVersion:"invalid",SSL:true,Application:&ApplicationHosting{Language:language,Enabled:enabled,Port:21000}}
  if err:=(&System{}).reconcileSiteRuntime(context.Background(),site,path);err!=nil{t.Fatal("application incorrectly entered PHP reconciliation",err)}
  content,_:=os.ReadFile(path);if strings.Contains(string(content),"fastcgi_pass")||!strings.Contains(string(content),"/current/cert.pem"){t.Fatal("application routing/TLS not retained")}
 })}}
 root:=t.TempDir();public:=filepath.Join(root,"website","public");if err:=os.MkdirAll(public,0750);err!=nil{t.Fatal(err)}
 if err:=(&System{}).reconcileSiteRuntime(context.Background(),Site{Domain:"php.example.test",Root:public},filepath.Join(root,"php.conf"));err==nil||!strings.Contains(err.Error(),"runtime identity is missing"){t.Fatal("ordinary website no longer enters PHP pool preparation",err)}
}
