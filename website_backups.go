package main

import (
 "archive/zip"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "time"
)

type websiteZIPBackup struct{ ID string `json:"id"`; SiteID string `json:"siteId"`; Domain string `json:"domain"`; Size int64 `json:"size"`; CreatedAt time.Time `json:"createdAt"` }
func loadWebsiteZIPBackup(root,id,siteID string)(websiteZIPBackup,error){
 var item websiteZIPBackup
 if !liveJobIDPattern.MatchString(id){return item,fmt.Errorf("invalid backup ID")}
 directory:=filepath.Join(root,id);info,err:=os.Lstat(directory);if err!=nil||!info.IsDir(){return item,fmt.Errorf("backup not found")}
 content,err:=os.ReadFile(filepath.Join(directory,"manifest.json"));if err!=nil||json.Unmarshal(content,&item)!=nil||item.ID!=id||item.SiteID!=siteID{return item,fmt.Errorf("backup not found for this website")}
 info,err=os.Lstat(filepath.Join(directory,"website.zip"));if err!=nil||!info.Mode().IsRegular()||info.Size()!=item.Size||item.Size<=0{return item,fmt.Errorf("backup archive is incomplete or unavailable")}
 return item,nil
}
func zipWebsite(ctx context.Context,root,destination string)error{
 output,err:=os.OpenFile(destination,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if err!=nil{return err};defer output.Close()
 archive:=zip.NewWriter(output)
 err=filepath.Walk(root,func(path string,info os.FileInfo,walkErr error)error{
  if walkErr!=nil{return walkErr};if err:=ctx.Err();err!=nil{return err}
  relative,err:=filepath.Rel(root,path);if err!=nil{return err};if relative=="."{return nil}
  if !info.Mode().IsRegular()&&!info.IsDir(){return fmt.Errorf("unsupported link or special file: %s; backup stopped",relative)}
  header,err:=zip.FileInfoHeader(info);if err!=nil{return err};header.Name="public_html/"+filepath.ToSlash(relative)
  if info.IsDir(){header.Name+="/"}else{header.Method=zip.Deflate}
  entry,err:=archive.CreateHeader(header);if err!=nil{return err};if info.IsDir(){return nil}
  input,err:=secureOpenManaged(root,filepath.ToSlash(relative),os.O_RDONLY,0);if err!=nil{return err};defer input.Close()
  _,err=io.Copy(entry,input);return err
 });closeErr:=archive.Close();if err!=nil{return err};return closeErr
}
func zipBackupParts(destination string,parts map[string]string)error{
 output,err:=os.OpenFile(destination,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if err!=nil{return err};defer output.Close();archive:=zip.NewWriter(output)
 for name,path:=range parts{entry,err:=archive.CreateHeader(&zip.FileHeader{Name:name,Method:zip.Store});if err!=nil{return err};input,err:=os.Open(path);if err!=nil{return err};_,copyErr:=io.Copy(entry,input);input.Close();if copyErr!=nil{return copyErr}}
 return archive.Close()
}
func (a *App) websiteBackups(w http.ResponseWriter,r *http.Request){
 site:=a.store.SiteByID(r.URL.Query().Get("siteId"));if site==nil{writeError(w,404,"Website not found.");return}
 root:=filepath.Join(a.system.dataDir,"website-backups",safeSiteID(site.ID));if err:=os.MkdirAll(root,0700);err!=nil{writeError(w,500,err.Error());return}
 if r.Method==http.MethodGet{
  if id:=r.URL.Query().Get("id");id!=""{if !liveJobIDPattern.MatchString(id){writeError(w,400,"Invalid backup ID");return};if _,err:=loadWebsiteZIPBackup(root,id,site.ID);err!=nil{writeError(w,404,"Complete backup not found for this website");return};file,err:=secureOpenManaged(root,id+"/website.zip",os.O_RDONLY,0);if err!=nil{writeError(w,404,"Backup not found");return};defer file.Close();info,err:=file.Stat();if err!=nil{writeError(w,500,"Cannot inspect backup");return};w.Header().Set("Content-Type","application/zip");w.Header().Set("Content-Disposition",fmt.Sprintf(`attachment; filename="%s-%s.zip"`,site.Domain,id));http.ServeContent(w,r,"website.zip",info.ModTime(),file);return}
  items:=[]websiteZIPBackup{};entries,_:=os.ReadDir(root);for _,entry:=range entries{if !entry.IsDir(){continue};if item,err:=loadWebsiteZIPBackup(root,entry.Name(),site.ID);err==nil{items=append(items,item)}};writeJSON(w,200,map[string]any{"items":items});return
 }
 if r.Method==http.MethodDelete{
  var input struct{ID string;Confirm bool};if decodeJSON(r,&input)!=nil||!input.Confirm||!liveJobIDPattern.MatchString(input.ID){writeError(w,400,"Confirm permanent backup deletion.");return}
  directory:=filepath.Join(root,input.ID);if _,err:=os.Stat(filepath.Join(directory,"manifest.json"));err!=nil{writeError(w,404,"Backup not found");return}
  // Only the explicitly selected generated backup is removed after confirmation.
  if err:=os.RemoveAll(directory);err!=nil{writeError(w,500,err.Error());return};writeJSON(w,200,map[string]bool{"ok":true});return
 }
 if r.Method!=http.MethodPost{writeError(w,405,"Use GET, POST or DELETE");return}
 if a.system.demo{writeError(w,400,"Backup generation requires the installed server");return}
 ctx,cancel:=context.WithTimeout(r.Context(),20*time.Minute);defer cancel()
 available,err:=migrationFreeBytes(ctx,root);if err!=nil||available<1<<30{writeError(w,409,"At least 1 GiB free space is required.");return}
 id,err:=randomToken(18);if err!=nil{writeError(w,500,"Cannot allocate backup");return};id=safeSiteID(id);directory:=filepath.Join(root,id)
 if err=os.Mkdir(directory,0700);err!=nil{writeError(w,500,err.Error());return}
 fail:=func(err error){writeError(w,502,"Backup stopped: "+err.Error()+". Partial files retained for review; no website data changed.")}
 if err=zipWebsite(ctx,site.Root,filepath.Join(directory,"public_html.zip"));err!=nil{fail(err);return}
 databases:=map[string]string{}
 for _,database:=range a.store.PublicState().Databases{if database.SiteID==site.ID{databases[database.Name]=database.Name}}
 if site.Database!=""{databases[site.Database]=site.Database}
 dumps:=map[string]string{}
 for name:=range databases{if !databaseObjectPattern.MatchString(name){fail(fmt.Errorf("invalid database name"));return};path:=filepath.Join(directory,name+".sql");_,err=runCommand(ctx,5*time.Minute,firstAvailableCommand("mysqldump","mariadb-dump"),"--protocol=socket","--user=root","--single-transaction","--routines","--triggers","--events","--result-file="+path,name);if err!=nil{fail(err);return};dumps[name+".sql"]=path}
 if len(dumps)==0{path:=filepath.Join(directory,"README.txt");if err=os.WriteFile(path,[]byte("No managed database is associated with this website. External or unregistered databases are not included."),0600);err!=nil{fail(err);return};dumps["README.txt"]=path}
 if err=zipBackupParts(filepath.Join(directory,"databases.zip"),dumps);err!=nil{fail(err);return}
 if err=zipBackupParts(filepath.Join(directory,"website.zip"),map[string]string{"public_html.zip":filepath.Join(directory,"public_html.zip"),"databases.zip":filepath.Join(directory,"databases.zip")});err!=nil{fail(err);return}
 info,err:=os.Stat(filepath.Join(directory,"website.zip"));if err!=nil{fail(err);return};item:=websiteZIPBackup{ID:id,SiteID:site.ID,Domain:site.Domain,Size:info.Size(),CreatedAt:time.Now().UTC()};content,_:=json.Marshal(item)
 if err=writeAtomic(filepath.Join(directory,"manifest.json"),content,0600);err!=nil{fail(err);return};writeJSON(w,201,item)
}
