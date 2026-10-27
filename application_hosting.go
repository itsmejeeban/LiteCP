package main

import("context";"io";"errors";"fmt";"hash/fnv";"net";"net/http";"os";"os/exec";"path/filepath";"regexp";"strings";"time")

type ApplicationHosting struct {Language string `json:"language"`;Version string `json:"version"`;Command string `json:"command"`;SetupCommand string `json:"setupCommand,omitempty"`;Port int `json:"port"`;Container string `json:"container,omitempty"`;Enabled bool `json:"enabled"`;Certificate string `json:"certificate,omitempty"`;CertificateKey string `json:"certificateKey,omitempty"`;Backup string `json:"backup,omitempty"`}
var hostingImages=map[string]map[string]string{
 "node":{"22":"docker.io/library/node:22-bookworm-slim","24":"docker.io/library/node:24-bookworm-slim"},
 "python":{"3.11":"docker.io/library/python:3.11-slim-bookworm","3.12":"docker.io/library/python:3.12-slim-bookworm","3.13":"docker.io/library/python:3.13-slim-bookworm","3.14":"docker.io/library/python:3.14-slim"},
 "ruby":{"3.3":"docker.io/library/ruby:3.3-slim","3.4":"docker.io/library/ruby:3.4-slim","4.0":"docker.io/library/ruby:4.0-slim"},
}
func validateApplicationHosting(site Site,app ApplicationHosting)error{
 if site.AppType!="regular"{return errors.New("use a Regular PHP/HTML website dedicated to this application; WordPress and WHMCS sites cannot be converted")}
 if site.RuntimeUID<1000||site.RuntimeGID<1000{return errors.New("website isolation identity is missing")}
 if hostingImages[app.Language][app.Version]==""{return errors.New("select a supported runtime version")}
 if strings.TrimSpace(app.Command)==""||len(app.Command)>2048||len(app.SetupCommand)>2048||strings.ContainsAny(app.Command+app.SetupCommand,"\x00\r\n"){return errors.New("provide a single-line startup command, at most 2048 characters")}
 return nil
}
func applicationContainerArgs(site Site,app ApplicationHosting,name,command string,server bool)[]string{
 args:=[]string{"run","--name",name,"--pull=never","--user",fmt.Sprintf("%d:%d",site.RuntimeUID,site.RuntimeGID),"--cap-drop=ALL","--security-opt=no-new-privileges","--security-opt=label=disable","--read-only","--tmpfs","/tmp:rw,nosuid,nodev,size=128m","--memory=256m","--cpus=1","--pids-limit=128","--workdir=/app","--volume",site.Root+":/app:rw","--env","PORT=8080","--env","HOST=0.0.0.0","--env","HOME=/app","--env","NODE_ENV=production","--env","PYTHONDONTWRITEBYTECODE=1","--env","PIP_TARGET=/app/.python-packages","--env","PYTHONPATH=/app/.python-packages","--env","GEM_HOME=/app/.gems","--env","GEM_PATH=/app/.gems:/usr/local/bundle"}
 if info,err:=os.Stat("/run/mysqld/mysqld.sock");err==nil&&info.Mode()&os.ModeSocket!=0{args=append(args,"--volume","/run/mysqld/mysqld.sock:/run/mysqld/mysqld.sock:ro","--env","DB_SOCKET=/run/mysqld/mysqld.sock")}
 if server{args=append(args,"--detach","--restart=no","--publish",fmt.Sprintf("127.0.0.1:%d:8080",app.Port))}
 return append(args,hostingImages[app.Language][app.Version],"/bin/sh","-lc",command)
}
func applicationPort(siteID string,sites []Site)(int,error){hash:=fnv.New32a();hash.Write([]byte(siteID));start:=20000+int(hash.Sum32()%20000);for offset:=0;offset<20000;offset++{port:=20000+(start-20000+offset)%20000;used:=false;for _,site:=range sites{if site.Application!=nil&&site.Application.Port==port{used=true}};if used{continue};listener,err:=net.Listen("tcp",fmt.Sprintf("127.0.0.1:%d",port));if err==nil{listener.Close();return port,nil}};return 0,errors.New("no free application port")}
func applicationReady(ctx context.Context,port int)error{deadline:=time.Now().Add(25*time.Second);for time.Now().Before(deadline){connection,err:=net.DialTimeout("tcp",fmt.Sprintf("127.0.0.1:%d",port),time.Second);if err==nil{connection.Close();return nil};select{case <-ctx.Done():return ctx.Err();case <-time.After(500*time.Millisecond):}};return errors.New("application did not listen on 0.0.0.0:8080; review its startup command and logs")}
func(s *System) applicationNginx(site Site)string{
 app:=site.Application;tls:="";if app.Certificate!=""&&app.CertificateKey!=""{tls=fmt.Sprintf("    listen 443 ssl;\n    listen [::]:443 ssl;\n    ssl_certificate %s;\n    ssl_certificate_key %s;",app.Certificate,app.CertificateKey)+"\n    if ($scheme = http) { return 301 https://$host$request_uri; }"}
 marker:="    add_header X-LiteCP-Application-State maintenance always;"
 routing:="    location / { return 503; }"
 if app.Enabled{marker="";routing=fmt.Sprintf(`    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }`,app.Port)}
 return fmt.Sprintf(`# Managed by LiteCP application hosting
server {
%s
    listen 80;
    listen [::]:80;
%s
    server_name %s;
    root %s;
%s
    client_max_body_size 64m;
%s
    location ^~ /.well-known/acme-challenge/ { try_files $uri =404; }
    location ~ /\.(?!well-known) { deny all; }
%s
}
`,wafBlock(site,site.WAFMode),tls,site.Domain,site.Root,siteLogDirectives(site),marker,routing)
}
func(s *System) writeApplicationProxy(ctx context.Context,site Site)error{
 if err:=s.writeApplicationRouting(ctx,site,[]byte(s.nginxConfig(site)));err!=nil{return err}
 if site.Application!=nil&&!site.Application.Enabled{return waitApplicationMaintenance(ctx,"http://127.0.0.1:80/",site.Domain,3*time.Second)};return nil
}
func waitApplicationMaintenance(ctx context.Context,endpoint,domain string,timeout time.Duration)error{
 ctx,cancel:=context.WithTimeout(ctx,timeout);defer cancel()
 transport:=&http.Transport{Proxy:nil,DisableKeepAlives:true};defer transport.CloseIdleConnections()
 client:=&http.Client{Transport:transport,Timeout:time.Second,CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}}
 for{
  request,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return err};request.Host=domain
  response,err:=client.Do(request)
  if err==nil{protected:=response.Header.Get("X-LiteCP-Application-State")=="maintenance"&&(response.StatusCode==http.StatusServiceUnavailable||response.StatusCode==http.StatusMovedPermanently);response.Body.Close();if protected{return nil}}
  select{case <-ctx.Done():return errors.New("maintenance routing has not become active; protected configuration retained, retry before uploading application source");case <-time.After(50*time.Millisecond):}
 }
}
func(s *System) writeApplicationRouting(ctx context.Context,site Site,next []byte)error{
 path,_:=s.nginxPaths(site.Domain);return writeApplicationRoutingFile(ctx,path,next)
}
func writeApplicationRoutingFile(ctx context.Context,path string,next []byte)error{
 original,err:=os.ReadFile(path);if err!=nil{return err}
 if err=writeAtomic(path,next,0644);err!=nil{return err}
 if _,err=runCommand(ctx,15*time.Second,"nginx","-t");err==nil{_,err=runCommand(ctx,15*time.Second,"systemctl","reload","nginx")}
 if err!=nil{if restoreErr:=writeAtomic(path,original,0644);restoreErr!=nil{return fmt.Errorf("%v; routing recovery failed: %w",err,restoreErr)};if _,restoreErr:=runCommand(context.Background(),15*time.Second,"systemctl","reload","nginx");restoreErr!=nil{return fmt.Errorf("%v; previous routing reload failed: %w",err,restoreErr)}};return err
}
var applicationCertificatePattern=regexp.MustCompile(`(?m)^\s*(ssl_certificate|ssl_certificate_key)\s+([^;\s]+);`)
func(s *System) preserveApplicationNginx(site Site,app *ApplicationHosting)([]byte,error){
 root:=filepath.Join(s.dataDir,"application-hosting",safeSiteID(site.ID));if err:=os.MkdirAll(root,0700);err!=nil{return nil,err}
 path,_:=s.nginxPaths(site.Domain);content,err:=os.ReadFile(path);if err!=nil{return nil,err}
 if err=updateApplicationCertificates(site,app,content);err!=nil{return nil,err}
 // Retain the original PHP/HTML routing for explicit owner-led recovery only.
 if app.Backup==""{id,err:=randomToken(8);if err!=nil{return nil,err};app.Backup=filepath.Join(root,"nginx-before-"+safeSiteID(id)+".conf");if err=os.WriteFile(app.Backup,content,0600);err!=nil{return nil,err}}
 return content,nil
}
func updateApplicationCertificates(site Site,app *ApplicationHosting,content []byte)error{
 // Read live certificate directives, including changes made by Certbot while disabled.
 app.Certificate="";app.CertificateKey=""
 for _,match:=range applicationCertificatePattern.FindAllStringSubmatch(string(content),-1){if strings.HasPrefix(match[2],"/")&&!strings.ContainsAny(match[2],"{}\"'"){if match[1]=="ssl_certificate"{app.Certificate=match[2]}else{app.CertificateKey=match[2]}}}
 if site.SSL&&(app.Certificate==""||app.CertificateKey==""){return errors.New("cannot preserve the website TLS certificate; no routing changes made")};return nil
}
func(a *App) applicationHosting(w http.ResponseWriter,r *http.Request){
 if r.Method==http.MethodGet{
  items:=[]map[string]any{};for _,site:=range a.store.Sites(){if site.Application!=nil{status:="disabled";if site.Application.Enabled{status="stopped";output,err:=a.system.applicationHostCommand(r.Context(),10*time.Second,"podman","inspect","--format","{{.State.Status}}",site.Application.Container);if err==nil{status=strings.TrimSpace(output)}};items=append(items,map[string]any{"siteId":site.ID,"domain":site.Domain,"application":site.Application,"status":status})}}
  versions:=map[string][]string{};for language,values:=range hostingImages{for version:=range values{versions[language]=append(versions[language],version)}}
  writeJSON(w,200,map[string]any{"enabled":a.store.PublicState().Settings.ApplicationHosting,"versions":versions,"items":items});return
 }
 if r.Method!=http.MethodPost{writeError(w,405,"Use GET or POST");return}
 var input struct{Action,Language,SiteID,Version,Command,SetupCommand string;Enabled,Confirm bool}
 if err:=decodeJSONWithLimit(r,&input,10<<10);err!=nil{writeError(w,400,err.Error());return}
 if hostingImages[input.Language]==nil{writeError(w,400,"Select Node.js, Python or Ruby");return}
 if a.system.demo{writeError(w,400,"Application hosting requires the installed Linux server");return}
 if input.Action=="feature"{
  if input.Enabled {if _,err:=exec.LookPath("podman");err!=nil{writeError(w,503,"Install application hosting dependencies with scripts/install-application-hosting.sh first");return}}
  if !input.Confirm{writeError(w,400,"Confirm the hosting feature change");return}
  if !input.Enabled{for _,site:=range a.store.Sites(){if site.Application!=nil&&site.Application.Language==input.Language&&site.Application.Enabled{writeError(w,409,"Disable this runtime on its websites before disabling the hosting feature");return}}}
  err:=a.store.UpdateSettings(func(settings *Settings){copy:=map[string]bool{};for key,value:=range settings.ApplicationHosting{copy[key]=value};copy[input.Language]=input.Enabled;settings.ApplicationHosting=copy});if err!=nil{writeError(w,500,err.Error());return};a.store.AddActivity("Application hosting feature changed",fmt.Sprintf("%s enabled=%t",input.Language,input.Enabled),"success");writeJSON(w,200,map[string]bool{"ok":true});return
 }
 site:=a.store.SiteByID(input.SiteID);if site==nil{writeError(w,404,"Website not found");return}
 if !a.store.PublicState().Settings.ApplicationHosting[input.Language]{writeError(w,409,"Enable this hosting feature first");return}
 save:=func()error{return a.store.UpdateSite(site.ID,func(value *Site){value.Application=site.Application})}
 if input.Action=="configure"{
  if site.Application!=nil&&site.Application.Enabled{writeError(w,409,"Disable the website runtime before changing its configuration");return}
  app:=ApplicationHosting{Language:input.Language,Version:input.Version,Command:strings.TrimSpace(input.Command),SetupCommand:strings.TrimSpace(input.SetupCommand)}
  if site.Application!=nil{app.Backup=site.Application.Backup}
  if err:=validateApplicationHosting(*site,app);err!=nil{writeError(w,400,err.Error());return}
  port,err:=applicationPort(site.ID,a.store.Sites());if err!=nil{writeError(w,500,err.Error());return};app.Port=port
  _,err=a.system.preserveApplicationNginx(*site,&app);if err!=nil{writeError(w,502,err.Error());return};site.Application=&app
  // Protect uploaded source immediately, before the first successful Start.
  if err=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{writeError(w,502,"Cannot protect application source; configuration not saved: "+err.Error());return}
  if err=save();err!=nil{writeError(w,500,"Application state save failed; maintenance routing remains active to protect source files. Original routing snapshot retained for owner recovery.");return};a.store.AddActivity("Application configured",site.Domain+": "+app.Language+" "+app.Version,"success");writeJSON(w,200,map[string]bool{"ok":true});return
 }
 if site.Application==nil||site.Application.Language!=input.Language{writeError(w,400,"Configure this website application first");return}
 app:=*site.Application;site.Application=&app
 if input.Action=="logs"{if app.Container==""{writeJSON(w,200,map[string]string{"logs":"No application container has been started."});return};output,err:=a.system.applicationHostCommand(r.Context(),15*time.Second,"podman","logs","--tail","150",app.Container);if err!=nil{writeError(w,502,"Application logs unavailable");return};writeJSON(w,200,map[string]string{"logs":output});return}
 if !input.Confirm{writeError(w,400,"Confirm application service changes");return}
 if err:=validateApplicationHosting(*site,app);err!=nil{writeError(w,400,err.Error());return}
 switch input.Action{
 case "start":
  if app.Enabled{
   if _,err:=a.system.preserveApplicationNginx(*site,&app);err!=nil{writeError(w,502,err.Error());return}
   if err:=a.system.startApplicationContainer(r.Context(),app);err!=nil{writeError(w,502,err.Error());return}
   if err:=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{_ = a.system.stopApplicationContainer(context.Background(),app.Container);writeError(w,502,"Cannot restore application routing: "+err.Error());return}
   if err:=save();err!=nil{writeError(w,500,"Application is running; certificate state could not be saved");return};writeJSON(w,200,map[string]bool{"ok":true});return
  }
  if _,err:=a.system.preserveApplicationNginx(*site,&app);err!=nil{writeError(w,502,err.Error());return}
  // Repair legacy disabled sites too, before dependency setup can write source.
  previous:=[]byte(a.system.nginxConfig(*site));if err:=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{writeError(w,502,"Cannot protect application source before startup: "+err.Error());return}
  if _,err:=a.system.applicationHostCommand(r.Context(),10*time.Minute,"podman","pull",hostingImages[app.Language][app.Version]);err!=nil{writeError(w,502,"Runtime image download failed; existing website unchanged");return}
  id,err:=randomToken(8);if err!=nil{writeError(w,500,err.Error());return};name:="litecp-app-"+safeSiteID(site.ID)+"-"+safeSiteID(id)
  if app.SetupCommand!=""{if _,err:=a.system.applicationHostCommand(r.Context(),10*time.Minute,"podman",applicationContainerArgs(*site,app,name+"-setup",app.SetupCommand,false)...);err!=nil{app.Container=name+"-setup";site.Application=&app;_ = save();writeError(w,502,"Dependency setup failed. Uploaded files and setup container retained for review. Open logs to inspect the setup failure.");return}}
  if _,err:=a.system.applicationHostCommand(r.Context(),time.Minute,"podman",applicationContainerArgs(*site,app,name,app.Command,true)...);err!=nil{writeError(w,502,"Application container could not start; website routing unchanged");return}
  if err:=applicationReady(r.Context(),app.Port);err!=nil{_ = a.system.stopApplicationContainer(context.Background(),name);app.Container=name;site.Application=&app;_ = save();writeError(w,502,err.Error());return}
  app.Container=name;app.Enabled=true;site.Application=&app
  if err:=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{_ = a.system.stopApplicationContainer(context.Background(),name);writeError(w,502,"Application started but proxy validation failed: "+err.Error());return}
  if err:=save();err!=nil{restoreErr:=a.system.writeApplicationRouting(context.Background(),*site,previous);_ = a.system.stopApplicationContainer(context.Background(),name);if restoreErr!=nil{writeError(w,500,"Application state save failed; previous routing recovery failed: "+restoreErr.Error());return};writeError(w,500,"Application state save failed; previous routing restored");return}
  if _,err:=a.system.applicationHostCommand(r.Context(),30*time.Second,"podman","update","--restart=always",name);err!=nil{writeError(w,502,"Application is running, but boot restart policy could not be enabled");return}
  if _,err:=a.system.applicationHostCommand(r.Context(),30*time.Second,"systemctl","enable","podman-restart.service");err!=nil{writeError(w,502,"Application is running, but boot restart could not be enabled; inspect podman-restart.service");return}
 case "stop":
  if app.Container!=""{if err:=a.system.stopApplicationContainer(r.Context(),app.Container);err!=nil{writeError(w,502,"Application stop failed: "+err.Error());return}}
 case "disable":
  if app.Container!=""{if err:=a.system.stopApplicationContainer(r.Context(),app.Container);err!=nil{writeError(w,502,"Application could not be stopped; check its container status: "+err.Error());return}}
  app.Enabled=false;site.Application=&app
  // Disabled application directories must never fall back to public static hosting.
  if _,err:=a.system.preserveApplicationNginx(*site,&app);err!=nil{writeError(w,502,err.Error());return}
  if err:=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{writeError(w,502,"Could not enable maintenance routing; review Nginx: "+err.Error());return}
  if err:=save();err!=nil{writeError(w,500,"Could not save application state; maintenance routing remains active to protect source files");return}
 default:writeError(w,400,"Unknown application action");return
 }
 a.store.AddActivity("Application hosting updated",site.Domain+": "+input.Language+" "+app.Version+" "+input.Action,"success")
 writeJSON(w,200,map[string]bool{"ok":true})
}

func(s *System) stopApplicationContainer(ctx context.Context,name string)error{
 // podman-restart.service otherwise revives explicitly stopped `always` containers.
 if _,err:=s.applicationHostCommand(ctx,30*time.Second,"podman","update","--restart=no",name);err!=nil{return fmt.Errorf("cannot disable boot restart: %w",err)}
 _,err:=s.applicationHostCommand(ctx,45*time.Second,"podman","stop",name);return err
}
func(s *System) startApplicationContainer(ctx context.Context,app ApplicationHosting)error{
 if _,err:=s.applicationHostCommand(ctx,time.Minute,"podman","start",app.Container);err!=nil{return errors.New("cannot start saved application container; check logs")}
 if err:=applicationReady(ctx,app.Port);err!=nil{_ = s.stopApplicationContainer(context.Background(),app.Container);return err}
 if _,err:=s.applicationHostCommand(ctx,30*time.Second,"podman","update","--restart=always",app.Container);err!=nil{return errors.New("application is running, but boot restart policy could not be enabled")}
 if _,err:=s.applicationHostCommand(ctx,30*time.Second,"systemctl","enable","podman-restart.service");err!=nil{return errors.New("application is running, but boot restart service could not be enabled")};return nil
}

// Detached container monitors may inherit --pipe file descriptors indefinitely.
// Route this feature's host-command output to a private file instead.
func(s *System) applicationHostCommand(ctx context.Context,timeout time.Duration,command string,args ...string)(string,error){
 root:=filepath.Join(s.dataDir,"application-hosting","command-logs");if err:=os.MkdirAll(root,0700);err!=nil{return "",err};id,err:=randomToken(12);if err!=nil{return "",err};path:=filepath.Join(root,safeSiteID(id)+".log")
 systemdArgs:=[]string{"--quiet","--wait","--collect","--service-type=exec","--property=KillMode=process","--property=TimeoutStopSec=10","--property=UMask=0077",fmt.Sprintf("--property=RuntimeMaxSec=%d",int(timeout.Seconds())),"--property=StandardOutput=append:"+path,"--property=StandardError=append:"+path,"--",command}
 safeArgs:=make([]string,len(args));for i,arg:=range args{safeArgs[i]=strings.ReplaceAll(arg,"$","$$")}
 _,err=runCommand(ctx,timeout,"systemd-run",append(systemdArgs,safeArgs...)...)
 return applicationCommandOutput(path,err)
}
func applicationCommandOutput(path string,commandErr error)(string,error){
 file,err:=os.Open(path);if err!=nil{if commandErr!=nil{return "",commandErr};return "",fmt.Errorf("read application command output: %w",err)};defer file.Close()
 output,err:=io.ReadAll(io.LimitReader(file,128<<10));if commandErr!=nil{return string(output),commandErr};if err!=nil{return string(output),fmt.Errorf("read application command output: %w",err)};return string(output),nil
}
