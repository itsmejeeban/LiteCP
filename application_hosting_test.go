package main
import("testing";"fmt";"os/exec";"os";"context";"path/filepath";"time";"strings";"encoding/json";"net/http/httptest")
func TestApplicationHostingDefaultsAndValidation(t *testing.T){var settings Settings;if err:=json.Unmarshal([]byte(`{}`),&settings);err!=nil{t.Fatal(err)};for language:=range hostingImages{if settings.ApplicationHosting[language]{t.Fatal("hosting enabled by default")}};site:=Site{AppType:"regular",RuntimeUID:15001,RuntimeGID:15001};app:=ApplicationHosting{Language:"node",Version:"24",Command:"node server.js"};if err:=validateApplicationHosting(site,app);err!=nil{t.Fatal(err)};site.AppType="wordpress";if validateApplicationHosting(site,app)==nil{t.Fatal("WordPress conversion allowed")};site.AppType="regular";app.Version="24;id";if validateApplicationHosting(site,app)==nil{t.Fatal("unreviewed image allowed")}}
func TestApplicationHostingContainerIsolation(t *testing.T){site:=Site{ID:"fixture",Root:"/var/www/litecp/example.test/public",RuntimeUID:15001,RuntimeGID:15001};app:=ApplicationHosting{Language:"python",Version:"3.13",Port:21000};args:=applicationContainerArgs(site,app,"fixture","python app.py",true);joined:=strings.Join(args," ");for _,required:=range []string{"--user 15001:15001","--cap-drop=ALL","--security-opt=no-new-privileges","--read-only","--memory=256m","--pids-limit=128","127.0.0.1:21000:8080","docker.io/library/python:3.13-slim-bookworm","/bin/sh -lc python app.py"}{if !strings.Contains(joined,required){t.Fatal("missing isolation",required)}};if strings.Contains(joined,"--privileged")||strings.Contains(joined,"--network=host"){t.Fatal("unsafe container permissions")}}
func TestApplicationNginxUsesProxyOnly(t *testing.T){system:=&System{};site:=Site{Domain:"example.test",Root:"/var/www/litecp/example.test/public",SSL:true,Application:&ApplicationHosting{Enabled:true,Port:21000,Certificate:"/etc/letsencrypt/live/example.test/fullchain.pem",CertificateKey:"/etc/letsencrypt/live/example.test/privkey.pem"}};config:=system.nginxConfig(site);if !strings.Contains(config,"proxy_pass http://127.0.0.1:21000")||!strings.Contains(config,"listen 443 ssl")||strings.Contains(config,"fastcgi_pass"){t.Fatal("application routing invalid")}}
func TestApplicationHostingRequiresAuthentication(t *testing.T){app:=&App{};response:=httptest.NewRecorder();app.routes().ServeHTTP(response,httptest.NewRequest("GET","/api/application-hosting",nil));if response.Code!=401{t.Fatal(response.Code)}}

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
 if call(`{"action":"configure","language":"node","siteID":"fixture-site","version":"24","command":"node server.js"}`)!=200{t.Fatal("application configuration failed")}
 reopened,err:=OpenStore(root);if err!=nil{t.Fatal(err)};if !reopened.PublicState().Settings.ApplicationHosting["node"]||reopened.SiteByID("fixture-site").Application.Version!="24"{t.Fatal("configuration not retained")}
 if reopened.PublicState().Settings.ApplicationHosting["python"]||reopened.PublicState().Settings.ApplicationHosting["ruby"]{t.Fatal("unselected runtimes enabled")}
 if call(`{"action":"feature","language":"node","enabled":false,"confirm":true}`)!=200{t.Fatal("feature could not be disabled")}
}
