package main

import (
 "context"
 "fmt"
 "net/http"
 "sort"
 "strconv"
 "strings"
 "time"
)

var phpSettingDefaults = map[string]string{
 "memory_limit":"256M", "upload_max_filesize":"128M", "post_max_size":"128M",
 "max_execution_time":"120", "max_input_time":"60", "max_input_vars":"1000",
 "display_errors":"Off", "log_errors":"On", "date.timezone":"Asia/Kolkata",
}

func validatePHPSettings(settings map[string]string) error {
 for key, value := range settings {
  if _, ok := phpSettingDefaults[key]; !ok { return fmt.Errorf("unsupported PHP setting: %s", key) }
  switch key {
  case "display_errors", "log_errors":
   if value != "On" && value != "Off" { return fmt.Errorf("%s must be On or Off", key) }
  case "date.timezone":
   if strings.ContainsAny(value,"\r\n\x00;[]\"") { return fmt.Errorf("invalid time zone") }
   if _, err := time.LoadLocation(value); err != nil { return fmt.Errorf("invalid time zone") }
  case "memory_limit", "upload_max_filesize", "post_max_size":
   if len(value)<2 || value[len(value)-1]!='M' { return fmt.Errorf("%s must use MiB, for example 256M",key) }
   n,err:=strconv.Atoi(value[:len(value)-1]);if err!=nil || n<1 || n>8192 {return fmt.Errorf("%s must be between 1M and 8192M",key)}
  default:
   n,err:=strconv.Atoi(value);if err!=nil || n<1 || n>100000 {return fmt.Errorf("%s must be between 1 and 100000",key)}
  }
 }
 get:=func(k string) int {v:=settings[k];if v=="" {v=phpSettingDefaults[k]};n,_:=strconv.Atoi(strings.TrimSuffix(v,"M"));return n}
 if get("post_max_size")<get("upload_max_filesize") {return fmt.Errorf("post_max_size must be at least upload_max_filesize")}
 return nil
}

func phpPoolSettings(settings map[string]string) string {
 // Imported snapshots must not inject arbitrary pool directives.
 if validatePHPSettings(settings)!=nil {return ""}
 keys:=make([]string,0,len(settings));for k:=range settings {keys=append(keys,k)};sort.Strings(keys)
 var result strings.Builder
 for _,k:=range keys {fmt.Fprintf(&result,"php_admin_value[%s] = %s\n",k,settings[k])}
 return result.String()
}

func (a *App) sitePHPSettings(w http.ResponseWriter,r *http.Request) {
 if r.Method!=http.MethodGet && r.Method!=http.MethodPut {writeError(w,405,"Use GET or PUT");return}
 site:=a.store.SiteByID(r.URL.Query().Get("siteId"));if site==nil {writeError(w,404,"Website not found");return}
 if r.Method==http.MethodGet {writeJSON(w,200,map[string]any{"settings":site.PHPSettings,"defaults":phpSettingDefaults,"version":site.PHPVersion});return}
 var input struct {Settings map[string]string `json:"settings"`}
 if decodeJSONWithLimit(r,&input,16<<10)!=nil {writeError(w,400,"Invalid settings");return}
 if err:=validatePHPSettings(input.Settings);err!=nil {writeError(w,400,err.Error());return}
 updated:=*site;updated.PHPSettings=input.Settings
 rollback:=func() error {ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel();return a.system.ensureSitePHPPool(ctx,*site)}
 if err:=a.system.ensureSitePHPPool(r.Context(),updated);err!=nil {
  if restoreErr:=rollback();restoreErr!=nil {writeError(w,500,"PHP settings failed; runtime recovery also failed. Check service logs.");return}
  writeError(w,502,"PHP settings could not be applied; previous settings restored.");return
 }
 if err:=a.store.UpdateSite(site.ID,func(value *Site){value.PHPSettings=input.Settings});err!=nil {
  if restoreErr:=rollback();restoreErr!=nil {writeError(w,500,"Saving failed; runtime recovery also failed. Check service logs.");return}
  writeError(w,500,"Saving failed; previous PHP settings restored.");return
 }
 _=a.store.AddActivity("PHP settings updated",site.Domain,"info")
 writeJSON(w,200,map[string]bool{"ok":true})
}
