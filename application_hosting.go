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
 if server{args=append(args,"--detach","--restart=always","--publish",fmt.Sprintf("127.0.0.1:%d:8080",app.Port))}
 return append(args,hostingImages[app.Language][app.Version],"/bin/sh","-lc",command)
}
func applicationPort(siteID string,sites []Site)(int,error){hash:=fnv.New32a();hash.Write([]byte(siteID));start:=20000+int(hash.Sum32()%20000);for offset:=0;offset<20000;offset++{port:=20000+(start-20000+offset)%20000;used:=false;for _,site:=range sites{if site.Application!=nil&&site.Application.Port==port{used=true}};if used{continue};listener,err:=net.Listen("tcp",fmt.Sprintf("127.0.0.1:%d",port));if err==nil{listener.Close();return port,nil}};return 0,errors.New("no free application port")}
func applicationReady(ctx context.Context,port int)error{deadline:=time.Now().Add(25*time.Second);for time.Now().Before(deadline){connection,err:=net.DialTimeout("tcp",fmt.Sprintf("127.0.0.1:%d",port),time.Second);if err==nil{connection.Close();return nil};select{case <-ctx.Done():return ctx.Err();case <-time.After(500*time.Millisecond):}};return errors.New("application did not listen on 0.0.0.0:8080; review its startup command and logs")}
func(s *System) applicationNginx(site Site)string{
 app:=site.Application;tls:="";if site.SSL&&app.Certificate!=""&&app.CertificateKey!=""{tls=fmt.Sprintf("    listen 443 ssl;\n    listen [::]:443 ssl;\n    ssl_certificate %s;\n    ssl_certificate_key %s;",app.Certificate,app.CertificateKey)+"\n    if ($scheme = http) { return 301 https://$host$request_uri; }"}
 return fmt.Sprintf(`# Managed by LiteCP application hosting
server {
%s
    listen 80;
    listen [::]:80;
%s
    server_name %s;
    root %s;
    client_max_body_size 64m;
    location ^~ /.well-known/acme-challenge/ { try_files $uri =404; }
    location ~ /\.(?!well-known) { deny all; }
    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
`,wafBlock(site,site.WAFMode),tls,site.Domain,site.Root,app.Port)
}
func(s *System) writeApplicationProxy(ctx context.Context,site Site)error{
 path,_:=s.nginxPaths(site.Domain);original,err:=os.ReadFile(path);if err!=nil{return err}
 next:=[]byte(s.nginxConfig(site))
 if site.Application!=nil&&!site.Application.Enabled&&site.Application.Backup!="" {backup:=site.Application.Backup;expected:=filepath.Join(s.dataDir,"application-hosting",safeSiteID(site.ID));if filepath.Dir(backup)!=expected{return errors.New("invalid application routing recovery path")};next,err=os.ReadFile(backup);if err!=nil{return err}}
 if err=writeAtomic(path,next,0644);err!=nil{return err}
 if _,err=runCommand(ctx,15*time.Second,"nginx","-t");err==nil{_,err=runCommand(ctx,15*time.Second,"systemctl","reload","nginx")}
 if err!=nil{_ = writeAtomic(path,original,0644);_,_=runCommand(context.Background(),15*time.Second,"systemctl","reload","nginx")};return err
}
var applicationCertificatePattern=regexp.MustCompile(`(?m)^\s*(ssl_certificate|ssl_certificate_key)\s+([^;\s]+);`)
func(s *System) preserveApplicationNginx(site Site,app *ApplicationHosting)error{
 root:=filepath.Join(s.dataDir,"application-hosting",safeSiteID(site.ID));if err:=os.MkdirAll(root,0700);err!=nil{return err}
 path,_:=s.nginxPaths(site.Domain);content,err:=os.ReadFile(path);if err!=nil{return err}
 // Keep every prior routing configuration for owner-led recovery.
 id,err:=randomToken(8);if err!=nil{return err};app.Backup=filepath.Join(root,"nginx-before-"+safeSiteID(id)+".conf");if err=os.WriteFile(app.Backup,content,0600);err!=nil{return err}
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
 old:=*site
 save:=func()error{return a.store.UpdateSite(site.ID,func(value *Site){value.Application=site.Application})}
 if input.Action=="configure"{
  if site.Application!=nil&&site.Application.Enabled{writeError(w,409,"Disable the website runtime before changing its configuration");return}
  app:=ApplicationHosting{Language:input.Language,Version:input.Version,Command:strings.TrimSpace(input.Command),SetupCommand:strings.TrimSpace(input.SetupCommand)}
  if err:=validateApplicationHosting(*site,app);err!=nil{writeError(w,400,err.Error());return}
  port,err:=applicationPort(site.ID,a.store.Sites());if err!=nil{writeError(w,500,err.Error());return};app.Port=port;site.Application=&app
  if err=save();err!=nil{writeError(w,500,err.Error());return};a.store.AddActivity("Application configured",site.Domain+": "+app.Language+" "+app.Version,"success");writeJSON(w,200,map[string]bool{"ok":true});return
 }
 if site.Application==nil||site.Application.Language!=input.Language{writeError(w,400,"Configure this website application first");return}
 app:=*site.Application;site.Application=&app
 if input.Action=="logs"{if app.Container==""{writeJSON(w,200,map[string]string{"logs":"No application container has been started."});return};output,err:=a.system.applicationHostCommand(r.Context(),15*time.Second,"podman","logs","--tail","150",app.Container);if err!=nil{writeError(w,502,"Application logs unavailable");return};writeJSON(w,200,map[string]string{"logs":output});return}
 if !input.Confirm{writeError(w,400,"Confirm application service changes");return}
 if err:=validateApplicationHosting(*site,app);err!=nil{writeError(w,400,err.Error());return}
 switch input.Action{
 case "start":
  if app.Enabled{
   if _,err:=a.system.applicationHostCommand(r.Context(),time.Minute,"podman","start",app.Container);err!=nil{writeError(w,502,"Cannot start saved application container; check logs");return};if err:=applicationReady(r.Context(),app.Port);err!=nil{writeError(w,502,err.Error());return};writeJSON(w,200,map[string]bool{"ok":true});return
  }
  if err:=a.system.preserveApplicationNginx(*site,&app);err!=nil{writeError(w,502,err.Error());return}
  if _,err:=a.system.applicationHostCommand(r.Context(),10*time.Minute,"podman","pull",hostingImages[app.Language][app.Version]);err!=nil{writeError(w,502,"Runtime image download failed; existing website unchanged");return}
  id,err:=randomToken(8);if err!=nil{writeError(w,500,err.Error());return};name:="litecp-app-"+safeSiteID(site.ID)+"-"+safeSiteID(id)
  if app.SetupCommand!=""{if _,err:=a.system.applicationHostCommand(r.Context(),10*time.Minute,"podman",applicationContainerArgs(*site,app,name+"-setup",app.SetupCommand,false)...);err!=nil{app.Container=name+"-setup";site.Application=&app;_ = save();writeError(w,502,"Dependency setup failed. Uploaded files and setup container retained for review. Open logs to inspect the setup failure.");return}}
  if _,err:=a.system.applicationHostCommand(r.Context(),time.Minute,"podman",applicationContainerArgs(*site,app,name,app.Command,true)...);err!=nil{writeError(w,502,"Application container could not start; website routing unchanged");return}
  if err:=applicationReady(r.Context(),app.Port);err!=nil{_,_=a.system.applicationHostCommand(context.Background(),30*time.Second,"podman","stop",name);app.Container=name;site.Application=&app;_ = save();writeError(w,502,err.Error());return}
  app.Container=name;app.Enabled=true;site.Application=&app
  if err:=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{_,_=a.system.applicationHostCommand(context.Background(),30*time.Second,"podman","stop",name);writeError(w,502,"Application started but proxy validation failed; previous routing restored");return}
  if err:=save();err!=nil{_ = a.system.writeApplicationProxy(context.Background(),old);_,_=a.system.applicationHostCommand(context.Background(),30*time.Second,"podman","stop",name);writeError(w,500,"Application state save failed; previous routing restored");return}
  if _,err:=a.system.applicationHostCommand(r.Context(),30*time.Second,"systemctl","enable","podman-restart.service");err!=nil{writeError(w,502,"Application is running, but boot restart could not be enabled; inspect podman-restart.service");return}
 case "stop":
  if app.Container!=""{if _,err:=a.system.applicationHostCommand(r.Context(),45*time.Second,"podman","stop",app.Container);err!=nil{writeError(w,502,"Application stop failed");return}}
 case "disable":
  if app.Container!=""{if _,err:=a.system.applicationHostCommand(r.Context(),45*time.Second,"podman","stop",app.Container);err!=nil{writeError(w,502,"Application could not be stopped; routing and feature state remain unchanged. Check its container status.");return}}
  app.Enabled=false;site.Application=&app
  // Preserve certificates when returning to the existing PHP/HTML host.
  if err:=a.system.writeApplicationProxy(r.Context(),*site);err!=nil{writeError(w,502,"Could not restore PHP/HTML routing; review Nginx");return}
  if err:=save();err!=nil{_ = a.system.writeApplicationProxy(context.Background(),old);writeError(w,500,"Could not save application state");return}
 default:writeError(w,400,"Unknown application action");return
 }
 a.store.AddActivity("Application hosting updated",site.Domain+": "+input.Language+" "+app.Version+" "+input.Action,"success")
 writeJSON(w,200,map[string]bool{"ok":true})
}

// Detached container monitors may inherit --pipe file descriptors indefinitely.
// Route this feature's host-command output to a private file instead.
func(s *System) applicationHostCommand(ctx context.Context,timeout time.Duration,command string,args ...string)(string,error){
 root:=filepath.Join(s.dataDir,"application-hosting","command-logs");if err:=os.MkdirAll(root,0700);err!=nil{return "",err};id,err:=randomToken(12);if err!=nil{return "",err};path:=filepath.Join(root,safeSiteID(id)+".log")
 systemdArgs:=[]string{"--quiet","--wait","--collect","--service-type=exec","--property=KillMode=process","--property=TimeoutStopSec=10","--property=UMask=0077",fmt.Sprintf("--property=RuntimeMaxSec=%d",int(timeout.Seconds())),"--property=StandardOutput=append:"+path,"--property=StandardError=append:"+path,"--",command}
 safeArgs:=make([]string,len(args));for i,arg:=range args{safeArgs[i]=strings.ReplaceAll(arg,"$","$$")}
 _,err=runCommand(ctx,timeout,"systemd-run",append(systemdArgs,safeArgs...)...)
 file,readErr:=os.Open(path);if readErr!=nil{return "",err};defer file.Close();output,_:=io.ReadAll(io.LimitReader(file,128<<10));return string(output),err
}
