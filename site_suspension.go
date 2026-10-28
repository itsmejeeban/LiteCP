package main
import("context";"fmt";"net/http";"os";"strings";"time")
const suspensionDirective="\n    # LITECP_SUSPENDED\n    default_type text/plain;\n    return 503 'This website is suspended. Please contact the administrator.';\n"
func suspensionConfig(content string,suspended bool)string{
 content=strings.ReplaceAll(content,suspensionDirective,"")
 if suspended{content=strings.ReplaceAll(content,"server {","server {"+suspensionDirective)};return content
}
func(s *System) setSuspension(ctx context.Context,site Site,suspended bool)error{
 if s.demo{return nil};path,_:=s.nginxPaths(site.Domain);old,err:=os.ReadFile(path);if err!=nil{return err}
 updated:=suspensionConfig(string(old),suspended);if updated==string(old){return nil}
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
