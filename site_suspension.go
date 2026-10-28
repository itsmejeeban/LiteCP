package main
import("context";"fmt";"net/http";"os";"strings";"time")
const legacySuspensionDirective="\n    # LITECP_SUSPENDED\n    default_type text/plain;\n    return 503 'This website is suspended. Please contact the administrator.';\n"
const suspensionPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>Website suspended</title><style>*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;background:#080d18;color:#eaf0ff;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background-image:radial-gradient(ellipse at 20% 10%,#16365480,transparent 55%),radial-gradient(ellipse at 90% 90%,#33204c70,transparent 50%)}main{width:100%;max-width:660px;padding:56px 42px;text-align:center;border:1px solid #354158;border-radius:28px;background:#111a2be8;box-shadow:0 28px 90px #0005}.icon{display:grid;place-items:center;width:88px;height:88px;margin:0 auto 28px;border-radius:26px;background:#f4b44b18;border:1px solid #f4b44b55;color:#f4b44b}svg{width:44px;height:44px}.label{display:inline-block;color:#efbf78;font-size:12px;letter-spacing:2px;font-weight:600;text-transform:uppercase}h1{font-size:clamp(30px,6vw,44px);letter-spacing:-1.5px;margin:16px 0}p{color:#afbdd2;font-size:16px;line-height:1.8;margin:0 auto;max-width:480px}.notice{margin:30px 0 0;padding:20px;border-radius:14px;background:#1b263a;color:#c2cde0;font-size:14px;line-height:1.7}footer{margin-top:30px;font-size:12px;color:#8392ab}@media(max-width:480px){main{padding:38px 24px;border-radius:20px}}</style></head><body><main><div class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="M12 3 3 7v5c0 5 9 9 9 9s9-4 9-9V7l-9-4Z"/><path d="M10 9v6m4-6v6"/></svg></div><span class="label">Access temporarily restricted</span><h1>This website is suspended</h1><p>This website is currently unavailable. If you own this website, please contact your hosting provider for help restoring access.</p><div class="notice">Visiting this website?<br>Please check back later. Only the website owner or hosting provider can resolve this restriction.</div><footer>Hosted with LiteCP</footer></main></body></html>`
const suspensionDirective="\n    # LITECP_SUSPENDED\n    default_type text/html;\n    location ^~ /.well-known/acme-challenge/ { try_files $uri =404; }\n    if ($uri !~ ^/\\.well-known/acme-challenge/) { return 503 '"+suspensionPage+"'; }\n"
func suspensionConfig(content string,suspended bool)string{
 content=strings.ReplaceAll(content,legacySuspensionDirective,"")
 content=strings.ReplaceAll(content,suspensionDirective,"")
 if suspended{content=strings.ReplaceAll(content,"server {","server {"+suspensionDirective)};return content
}
const suspensionTLSMarker = "# LITECP_SUSPENSION_TLS_GUARD\n"
func suspensionSiteConfig(content string, site Site, suspended bool) string {
 if i:=strings.Index(content,suspensionTLSMarker);i>=0 {content=content[:i]}
 content=suspensionConfig(content,suspended)
 // An HTTP-only domain must never fall through to another website's TLS host.
 if suspended && !strings.Contains(content,"ssl_certificate ") && domainPattern.MatchString(site.Domain) {
  content+=suspensionTLSMarker+"server {\n    listen 443 ssl;\n    listen [::]:443 ssl;\n    server_name "+site.Domain+";\n    ssl_reject_handshake on;\n}\n"
 }
 return content
}
func(s *System) setSuspension(ctx context.Context,site Site,suspended bool)error{
 if s.demo{return nil};path,_:=s.nginxPaths(site.Domain);old,err:=os.ReadFile(path);if err!=nil{return err}
 updated:=suspensionSiteConfig(string(old),site,suspended);if updated==string(old){return nil}
 if err=writeAtomic(path,[]byte(updated),0644);err!=nil{return err}
 rollback:=func(cause error)error{restore:=writeAtomic(path,old,0644);_,reload:=runCommand(context.Background(),15*time.Second,"systemctl","reload","nginx");return fmt.Errorf("suspension change failed: %v (restore: %v, reload: %v)",cause,restore,reload)}
 if _,err=runCommand(ctx,15*time.Second,"nginx","-t");err!=nil{return rollback(err)}
 if _,err=runCommand(ctx,15*time.Second,"systemctl","reload","nginx");err!=nil{return rollback(err)};return nil
}
func(a *App) siteSuspension(w http.ResponseWriter,r *http.Request){
 var input struct{ID string `json:"id"`;Suspended bool `json:"suspended"`};if err:=decodeJSON(r,&input);err!=nil{writeError(w,400,err.Error());return}
 site:=a.store.SiteByID(input.ID);if site==nil{writeError(w,404,"Website not found.");return}
 if err:=a.system.setSuspension(r.Context(),*site,input.Suspended);err!=nil{writeError(w,502,err.Error());return}
 if err:=a.store.UpdateSite(site.ID,func(s *Site){s.Suspended=input.Suspended});err!=nil{rollback:=a.system.setSuspension(context.Background(),*site,site.Suspended);writeError(w,500,fmt.Sprintf("Cannot save suspension: %v (rollback: %v)",err,rollback));return}
 action:="Website unsuspended";if input.Suspended{action="Website suspended"};a.store.AddActivity(action,site.Domain,"info");writeJSON(w,200,map[string]bool{"suspended":input.Suspended})
}
