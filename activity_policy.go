package main
import("net/http";"time")
func (a *App) activityPolicy(w http.ResponseWriter,r *http.Request){
 if r.Method==http.MethodGet{writeJSON(w,200,map[string]int{"days":a.store.PublicState().Settings.ActivityRetentionDays});return}
 var input struct{Days int;Confirm bool}
 if decodeJSON(r,&input)!=nil||!input.Confirm{writeError(w,400,"Confirm activity retention or deletion.");return}
 if r.Method!=http.MethodPut&&r.Method!=http.MethodDelete{writeError(w,405,"Use GET, PUT or DELETE");return}
 valid:=false;for _,days:=range []int{0,7,15,30,60,365}{if input.Days==days{valid=true}}
 if !valid{writeError(w,400,"Unsupported retention period");return}
 s:=a.store;s.mu.Lock();defer s.mu.Unlock()
 previous:=append([]Activity{},s.cfg.Activities...);previousDays:=s.cfg.Settings.ActivityRetentionDays
 if r.Method==http.MethodDelete{s.cfg.Activities=[]Activity{}}else{
 s.cfg.Settings.ActivityRetentionDays=input.Days
 if input.Days>0{cutoff:=time.Now().UTC().AddDate(0,0,-input.Days);kept:=[]Activity{};for _,item:=range s.cfg.Activities{if !item.CreatedAt.Before(cutoff){kept=append(kept,item)}};s.cfg.Activities=kept}
 }
 if err:=s.saveLocked();err!=nil{s.cfg.Activities=previous;s.cfg.Settings.ActivityRetentionDays=previousDays;writeError(w,500,"Could not save activity policy");return}
 writeJSON(w,200,map[string]bool{"ok":true})
}
