package main
import("testing";"fmt";"os/exec";"os";"context";"path/filepath";"time";"strings";"encoding/json";"net/http/httptest";"net/http";"errors";"runtime";"sync/atomic")
func TestApplicationHostingDefaultsAndValidation(t *testing.T){var settings Settings;if err:=json.Unmarshal([]byte(`{}`),&settings);err!=nil{t.Fatal(err)};for language:=range hostingImages{if settings.ApplicationHosting[language]{t.Fatal("hosting enabled by default")}};site:=Site{AppType:"regular",RuntimeUID:15001,RuntimeGID:15001};app:=ApplicationHosting{Language:"node",Version:"24",Command:"node server.js"};if err:=validateApplicationHosting(site,app);err!=nil{t.Fatal(err)};site.AppType="wordpress";if validateApplicationHosting(site,app)==nil{t.Fatal("WordPress conversion allowed")};site.AppType="regular";app.Version="24;id";if validateApplicationHosting(site,app)==nil{t.Fatal("unreviewed image allowed")}}
func TestApplicationHostingContainerIsolation(t *testing.T){site:=Site{ID:"fixture",Root:"/var/www/litecp/example.test/public",RuntimeUID:15001,RuntimeGID:15001};app:=ApplicationHosting{Language:"python",Version:"3.13",Port:21000};args:=applicationContainerArgs(site,app,"fixture","python app.py",true);joined:=strings.Join(args," ");for _,required:=range []string{"--user 15001:15001","--cap-drop=ALL","--security-opt=no-new-privileges","--read-only","--memory=256m","--pids-limit=128","--restart=no","127.0.0.1:21000:8080","docker.io/library/python:3.13-slim-bookworm","/bin/sh -lc python app.py"}{if !strings.Contains(joined,required){t.Fatal("missing isolation",required)}};if strings.Contains(joined,"--privileged")||strings.Contains(joined,"--network=host"){t.Fatal("unsafe container permissions")}}
func TestApplicationNginxUsesProxyOnly(t *testing.T){system:=&System{};site:=Site{Domain:"example.test",Root:"/var/www/litecp/example.test/public",SSL:true,Application:&ApplicationHosting{Enabled:true,Port:21000,Certificate:"/etc/letsencrypt/live/example.test/fullchain.pem",CertificateKey:"/etc/letsencrypt/live/example.test/privkey.pem"}};config:=system.nginxConfig(site);if !strings.Contains(config,"proxy_pass http://127.0.0.1:21000")||!strings.Contains(config,"listen 443 ssl")||strings.Contains(config,"fastcgi_pass"){t.Fatal("application routing invalid")}}
func TestApplicationHostingRequiresAuthentication(t *testing.T){app:=&App{};response:=httptest.NewRecorder();app.routes().ServeHTTP(response,httptest.NewRequest("GET","/api/application-hosting",nil));if response.Code!=401{t.Fatal(response.Code)}}

func TestApplicationMaintenanceWaitsForLoadedHostRouting(t *testing.T){
 var requests int32;server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.Host!="application.example.test"{t.Errorf("wrong host %q",r.Host)};if atomic.AddInt32(&requests,1)<3{w.WriteHeader(200);return};w.Header().Set("X-LiteCP-Application-State","maintenance");w.WriteHeader(503)}));defer server.Close()
 if err:=waitApplicationMaintenance(context.Background(),server.URL,"application.example.test",time.Second);err!=nil{t.Fatal(err)};if atomic.LoadInt32(&requests)<3{t.Fatal("accepted old static routing")}
}
func TestApplicationMaintenanceRedirectDoesNotFollowTLS(t *testing.T){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("X-LiteCP-Application-State","maintenance");w.Header().Set("Location","https://unreachable.invalid/");w.WriteHeader(301)}));defer server.Close()
 if err:=waitApplicationMaintenance(context.Background(),server.URL,"application.example.test",time.Second);err!=nil{t.Fatal("loaded same-server maintenance redirect rejected",err)}
}
func TestApplicationMaintenanceRejectsUnmarkedResponse(t *testing.T){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(503)}));defer server.Close()
 if err:=waitApplicationMaintenance(context.Background(),server.URL,"application.example.test",120*time.Millisecond);err==nil{t.Fatal("unverified server response accepted")}
}

func TestDisabledApplicationNeverServesSource(t *testing.T){
 system:=&System{};for language:=range hostingImages{site:=Site{Domain:"example.test",Root:"/var/www/litecp/example.test/public",Application:&ApplicationHosting{Language:language,Enabled:false,Port:21000,Certificate:"/live/new/fullchain.pem",CertificateKey:"/live/new/privkey.pem",Backup:"/saved/prior-php.conf"}};config:=system.nginxConfig(site)
 if !strings.Contains(config,"location / { return 503; }")||!strings.Contains(config,"listen 443 ssl")||!strings.Contains(config,"/live/new/fullchain.pem"){t.Fatalf("%s disabled protection or live TLS missing: %s",language,config)}
 for _,unsafe:=range []string{"proxy_pass","fastcgi_pass","index.php","location / {\n        try_files"}{if strings.Contains(config,unsafe){t.Fatalf("%s disabled application has public source routing: %s",language,unsafe)}}
 if !strings.Contains(config,"/.well-known/acme-challenge/"){t.Fatal("ACME renewal route lost")}}
}
func TestApplicationCertificatesUseCurrentRouting(t *testing.T){
 app:=ApplicationHosting{Certificate:"/old/cert.pem",CertificateKey:"/old/key.pem"};content:=[]byte("server {\n ssl_certificate /new/cert.pem;\n ssl_certificate_key /new/key.pem;\n}\n")
 if err:=updateApplicationCertificates(Site{SSL:true},&app,content);err!=nil{t.Fatal(err)};if app.Certificate!="/new/cert.pem"||app.CertificateKey!="/new/key.pem"{t.Fatal("stale TLS metadata retained")}
 if err:=updateApplicationCertificates(Site{SSL:true},&app,[]byte("server { listen 80; }"));err==nil{t.Fatal("missing live TLS accepted for SSL website")}
 if app.Certificate!=""||app.CertificateKey!=""{t.Fatal("stale certificate survived missing routing")}
}
func TestApplicationCommandOutputFailsClosed(t *testing.T){
 path:=filepath.Join(t.TempDir(),"output.log");if _,err:=applicationCommandOutput(path,nil);err==nil{t.Fatal("missing command output reported success")}
 commandErr:=errors.New("container command failed");if _,err:=applicationCommandOutput(path,commandErr);!errors.Is(err,commandErr){t.Fatal("command error lost",err)}
 if err:=os.WriteFile(path,[]byte("container output"),0600);err!=nil{t.Fatal(err)};if output,err:=applicationCommandOutput(path,nil);err!=nil||output!="container output"{t.Fatal("valid command output lost",output,err)}
 if output,err:=applicationCommandOutput(path,commandErr);!errors.Is(err,commandErr)||output!="container output"{t.Fatal("failed command output lost",output,err)}
}
func TestApplicationRoutingFailureRestoresExactTLS(t *testing.T){
 if runtime.GOOS=="windows"{t.Skip("POSIX command fixtures")};root:=t.TempDir();bin:=filepath.Join(root,"bin");if err:=os.Mkdir(bin,0700);err!=nil{t.Fatal(err)}
 for name,script:=range map[string]string{"nginx":"#!/bin/sh\nexit 1\n","systemctl":"#!/bin/sh\nexit 0\n"}{if err:=os.WriteFile(filepath.Join(bin,name),[]byte(script),0700);err!=nil{t.Fatal(err)}};t.Setenv("PATH",bin)
 path:=filepath.Join(root,"site.conf");original:="server {\n listen 443 ssl;\n ssl_certificate /current/cert.pem;\n ssl_certificate_key /current/key.pem;\n # Owner custom routing\n}\n";if err:=os.WriteFile(path,[]byte(original),0600);err!=nil{t.Fatal(err)}
 if err:=writeApplicationRoutingFile(context.Background(),path,[]byte("server { listen 80; }"));err==nil{t.Fatal("invalid replacement reported success")};restored,err:=os.ReadFile(path);if err!=nil||string(restored)!=original{t.Fatal("failed routing change lost exact TLS configuration",err)}
}
func TestApplicationDemoDetachDoesNotRunHostCommands(t *testing.T){
 system:=&System{demo:true};site:=Site{Domain:"example.test",Application:&ApplicationHosting{Container:"fixture"}};if err:=system.RemoveSiteConfig(context.Background(),site);err!=nil{t.Fatal("demo detach invoked container commands",err)}
}
func TestApplicationNginxParserFixtures(t *testing.T){
 binary,err:=exec.LookPath("nginx");if err!=nil{t.Skip("nginx parser unavailable")};root:=t.TempDir();system:=&System{}
 for _,enabled:=range []bool{false,true}{t.Run(fmt.Sprintf("enabled=%t",enabled),func(t *testing.T){
  site:=Site{Domain:"application-fixture.test",Root:filepath.Join(root,"public"),Application:&ApplicationHosting{Enabled:enabled,Port:21000}}
  config:=fmt.Sprintf("pid %q;\nerror_log %q;\nevents {}\nhttp {\naccess_log off;\nclient_body_temp_path %q;\nproxy_temp_path %q;\nfastcgi_temp_path %q;\nuwsgi_temp_path %q;\nscgi_temp_path %q;\n%s\n}\n",filepath.Join(root,"nginx.pid"),filepath.Join(root,"error.log"),filepath.Join(root,"client-body"),filepath.Join(root,"proxy"),filepath.Join(root,"fastcgi"),filepath.Join(root,"uwsgi"),filepath.Join(root,"scgi"),system.nginxConfig(site))
  // This fixture validates application routing with WAF disabled; host policy and dynamic modules are tested separately.
  config=strings.Replace(config,wafBlock(site,site.WAFMode),legacyWAFBlock(site,"off"),1)
  path:=filepath.Join(root,fmt.Sprintf("nginx-%t.conf",enabled));if err:=os.WriteFile(path,[]byte(config),0600);err!=nil{t.Fatal(err)}
  output,err:=exec.Command(binary,"-t","-p",root+string(os.PathSeparator),"-c",path).CombinedOutput();if err!=nil{t.Fatalf("isolated application Nginx config rejected: %v\n%s",err,output)}
 })}
}

func TestApplicationHostingLiveContainerFixtures(t *testing.T){
 if os.Getenv("LITECP_TEST_APPLICATIONS")!="1"{t.Skip("explicit live container fixture check")}
 for _,fixture:=range []struct{language,version,file,command,code string}{
  {"node","24","server.js",`test "$PORT" = 8080 && node server.js`,`require('http').createServer((req,res)=>res.end('litecp-node-fixture')).listen(8080,'0.0.0.0');`},
  {"python","3.13","app.py","python app.py",`from http.server import HTTPServer, BaseHTTPRequestHandler
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200); self.end_headers(); self.wfile.write(b'litecp-python-fixture')
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
`},
  {"ruby","3.4","app.rb","ruby app.rb",`require 'socket'
server=TCPServer.new('0.0.0.0',8080)
loop do
 client=server.accept
 client.gets
 body='litecp-ruby-fixture'
 client.write("HTTP/1.1 200 OK\r\nContent-Length: #{body.bytesize}\r\nConnection: close\r\n\r\n#{body}") rescue nil
 client.close
end
`},
 }{
 t.Run(fixture.language,func(t *testing.T){
 ctx:=context.Background();root:=t.TempDir();if err:=os.Chown(root,15001,15001);err!=nil{t.Fatal(err)};if err:=os.Chmod(root,0750);err!=nil{t.Fatal(err)}
 source:=filepath.Join(root,fixture.file);if err:=os.WriteFile(source,[]byte(fixture.code),0644);err!=nil{t.Fatal(err)}
 app:=ApplicationHosting{Language:fixture.language,Version:fixture.version};port,err:=applicationPort("fixture-"+fixture.language,nil);if err!=nil{t.Fatal(err)};app.Port=port
 site:=Site{ID:"fixture",Root:root,RuntimeUID:15001,RuntimeGID:15001};name:="litecp-fixture-"+fixture.language+"-"+time.Now().Format("150405")
 if _,err=(&System{dataDir:"/var/lib/litecp"}).applicationHostCommand(ctx,10*time.Minute,"podman","pull",hostingImages[fixture.language][fixture.version]);err!=nil{t.Fatal(err)}
 if _,err=(&System{dataDir:"/var/lib/litecp"}).applicationHostCommand(ctx,time.Minute,"podman",applicationContainerArgs(site,app,name,fixture.command,true)...);err!=nil{t.Fatal(err)}
 defer (&System{dataDir:"/var/lib/litecp"}).applicationHostCommand(ctx,30*time.Second,"podman","stop",name)
 if err=applicationReady(ctx,port);err!=nil{t.Fatal(err)}
 output,err:=runCommand(ctx,5*time.Second,"curl","--fail","--silent",fmt.Sprintf("http://127.0.0.1:%d",port));if err!=nil||!strings.Contains(output,"litecp-"+fixture.language+"-fixture"){t.Fatalf("fixture HTTP check failed: %v",err)}
 t.Logf("%s %s: isolated HTTP application responded on loopback",fixture.language,fixture.version)
 })}
}

func TestApplicationHostingFeatureGateAndPersistence(t *testing.T){
 if _,err:=exec.LookPath("podman");err!=nil{t.Skip("container engine required for feature enable validation")}
 root:=t.TempDir();store,err:=OpenStore(root);if err!=nil{t.Fatal(err)};store.cfg.Sites=[]Site{{ID:"fixture-site",Domain:"fixture.test",Root:t.TempDir(),AppType:"regular",RuntimeUID:15001,RuntimeGID:15001}}
 app:=&App{store:store,system:&System{dataDir:root}}
 call:=func(body string)int{response:=httptest.NewRecorder();app.applicationHosting(response,httptest.NewRequest("POST","/api/application-hosting",strings.NewReader(body)));return response.Code}
 if call(`{"action":"start","language":"node","siteID":"fixture-site","confirm":true}`)!=409{t.Fatal("disabled runtime could start")}
 if call(`{"action":"feature","language":"node","enabled":true,"confirm":true}`)!=200{t.Fatal("feature could not be enabled")}
 reopened,err:=OpenStore(root);if err!=nil{t.Fatal(err)};if !reopened.PublicState().Settings.ApplicationHosting["node"]{t.Fatal("feature setting not retained")}
 if reopened.PublicState().Settings.ApplicationHosting["python"]||reopened.PublicState().Settings.ApplicationHosting["ruby"]{t.Fatal("unselected runtimes enabled")}
 if call(`{"action":"feature","language":"node","enabled":false,"confirm":true}`)!=200{t.Fatal("feature could not be disabled")}
}
