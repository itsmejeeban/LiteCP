package main

import (
 "context"
 "fmt"
 "net/http"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
)

// Configuration is inspected as text; source PHP is never executed as root.
func migrationDBName(content,app string)(string,error){
 pattern:=`(?m)define\s*\(\s*['"]DB_NAME['"]\s*,\s*['"]([A-Za-z0-9_$-]+)['"]\s*\)`
 if app=="whmcs"{pattern=`(?m)\$db_name\s*=\s*['"]([A-Za-z0-9_$-]+)['"]\s*;`}
 match:=regexp.MustCompile(pattern).FindStringSubmatch(content);if len(match)!=2{return "",fmt.Errorf("application database name is not a supported literal; manual configuration required")};return match[1],nil
}
func rewriteMigrationDB(content,app string,credentials DBCredentials)(string,error){
 values:=map[string]string{"DB_NAME":credentials.Name,"DB_USER":credentials.Username,"DB_PASSWORD":credentials.Password,"DB_HOST":"localhost"}
 if app=="whmcs"{values=map[string]string{"db_name":credentials.Name,"db_username":credentials.Username,"db_password":credentials.Password,"db_host":"localhost"}}
 for key,value:=range values{
  escaped:=strings.ReplaceAll(strings.ReplaceAll(value,`\`,`\\`),`'`,`\'`)
  pattern:=`(?m)define\s*\(\s*['"]`+key+`['"]\s*,\s*(?:'[^'\\]*(?:\\.[^'\\]*)*'|"[^"\\]*(?:\\.[^"\\]*)*")\s*\)`
  replacement:="define('"+key+"', '"+escaped+"')"
  if app=="whmcs"{pattern=`(?m)\$`+key+`\s*=\s*(?:'[^'\\]*(?:\\.[^'\\]*)*'|"[^"\\]*(?:\\.[^"\\]*)*")\s*;`;replacement="$"+key+" = '"+escaped+"';"}
  re:=regexp.MustCompile(pattern);if len(re.FindAllStringIndex(content,-1))!=1{return "",fmt.Errorf("unsupported or ambiguous %s configuration; manual review required",key)}
  content=re.ReplaceAllStringFunc(content,func(string)string{return replacement})
 };return content,nil
}

func(a *App) startAutoMigrationRestores(){if a.system.demo{return};go func(){timer:=time.NewTicker(10*time.Second);defer timer.Stop();for range timer.C{
 if !a.mutationMu.TryLock(){continue}
 entries,_:=os.ReadDir(filepath.Join(a.system.dataDir,"live-migrations"));for _,entry:=range entries{job,err:=a.loadLiveJob(entry.Name());if err!=nil||!job.AutoRestore||job.Status!="review"{continue};a.performAutoMigration(&job,false)}
 a.mutationMu.Unlock()
 }}()}

func(a *App) autoMigrationAction(w http.ResponseWriter,r *http.Request){
 var input struct{ID string `json:"id"`;Overwrite bool `json:"overwrite"`;ConfirmDomain string `json:"confirmDomain"`}
 if err:=decodeJSON(r,&input);err!=nil{writeError(w,400,err.Error());return}
 job,err:=a.loadLiveJob(input.ID);if err!=nil{writeError(w,404,err.Error());return}
 domain:=migrationJobDomain(job)
 if job.Status=="captured"{if err=a.stageRetainedCapture(&job);err!=nil{writeError(w,409,err.Error());return}}
 if job.Status!="review"&&job.Status!="awaiting-overwrite"{writeError(w,409,"Capture must finish and be ready for restore first.");return}
 if input.Overwrite&&input.ConfirmDomain!=domain{writeError(w,400,"Type the exact domain to confirm overwrite.");return}
 job.AutoRestore=true;a.performAutoMigration(&job,input.Overwrite);writeJSON(w,200,job)
}
func migrationJobDomain(job LiveMigrationJob)string{for _,account:=range job.Accounts{if account.ID==job.Account{return strings.ToLower(account.Domain)}};return ""}

func(a *App) performAutoMigration(job *LiveMigrationJob,overwrite bool){
 fail:=func(err error){job.Status="restore-review";job.Message="Automatic restore stopped: "+err.Error()+". Retained backup and existing data were preserved.";_ = a.saveLiveJob(job)}
 domain:=migrationJobDomain(*job);if !domainPattern.MatchString(domain){fail(fmt.Errorf("source domain requires manual mapping"));return}
 existing:=a.store.SiteByDomain(domain)
 if existing!=nil&&!overwrite{job.Status="awaiting-overwrite";job.Message="Domain already exists. Confirm overwrite to create a recovery backup and replace its files; existing databases will be retained.";_ = a.saveLiveJob(job);return}
 preview,root,err:=a.loadMigration(job.ID);if err!=nil{fail(err);return};if len(preview.Roots)!=1{fail(fmt.Errorf("multiple or missing website roots require manual mapping"));return}
 source:=filepath.Join(root,"files",filepath.FromSlash(preview.Roots[0]));app,configName:="regular",""
 if info,e:=os.Lstat(filepath.Join(source,"wp-config.php"));e==nil&&info.Mode().IsRegular(){app,configName="wordpress","wp-config.php"}else if info,e:=os.Lstat(filepath.Join(source,"configuration.php"));e==nil&&info.Mode().IsRegular(){app,configName="whmcs","configuration.php"}
 content:="";sqlPath:=""
 if configName!=""{data,e:=os.ReadFile(filepath.Join(source,configName));if e!=nil{fail(e);return};content=string(data);db,e:=migrationDBName(content,app);if e!=nil{fail(e);return};for _,dump:=range preview.Databases{if filepath.Base(dump)==db+".sql"{if sqlPath!=""{fail(fmt.Errorf("ambiguous database dump"));return};sqlPath=filepath.Join(root,"files",filepath.FromSlash(dump))}};if sqlPath==""{fail(fmt.Errorf("matching database dump was not found"));return}
 }else if len(preview.Databases)>0{fail(fmt.Errorf("custom application database configuration needs a supported adapter"));return}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Minute);defer cancel()
 job.Status="restoring";job.Message="Restoring website files and database. DNS remains unchanged.";_ = a.saveLiveJob(job)
 if existing!=nil{backup,e:=a.system.CreateBackup(ctx,*existing);if e!=nil{fail(fmt.Errorf("recovery backup failed: %w",e));return};if e=a.store.AddBackup(backup);e!=nil{fail(e);return}}
 var site Site
 if existing==nil{site,_,err=a.system.CreateSite(ctx,CreateSiteInput{Domain:domain,AppType:app,AppVersion:"9.0",PHPVersion:"8.3"});if err!=nil{fail(err);return};if err=a.store.AddSiteWithDatabase(site,nil);err!=nil{fail(err);return}}else{site=*existing}
 stage,err:=os.MkdirTemp(filepath.Dir(site.Root),".auto-migration-");if err!=nil{fail(err);return};if err=os.Chmod(stage,0750);err!=nil{fail(err);return};if err=os.Chown(stage,site.RuntimeUID,site.RuntimeGID);err!=nil{fail(err);return}
 staged:=site;staged.Root=stage;if err=copyMigrationTree(ctx,source,staged,site.RuntimeUID,site.RuntimeGID);err!=nil{fail(err);return}
 var database *ManagedDatabase
 if sqlPath!=""{
  credentials,e:=a.system.createDatabase(ctx,domain,site.PHPVersion);if e!=nil{fail(e);return};id,e:=randomToken(8);if e!=nil{fail(e);return};value:=ManagedDatabase{ID:id,SiteID:site.ID,Domain:domain,Name:credentials.Name,Username:credentials.Username,CreatedAt:time.Now().UTC()};database=&value
  if e=a.store.AddDatabase(value);e!=nil{fail(e);return};if e=a.system.importScopedSQL(ctx,sqlPath,value,credentials.Password);e!=nil{fail(e);return}
  rewritten,e:=rewriteMigrationDB(content,app,credentials);if e!=nil{fail(e);return};if e=writeAtomic(filepath.Join(stage,configName),[]byte(rewritten),0640);e!=nil{fail(e);return};if e=os.Chown(filepath.Join(stage,configName),site.RuntimeUID,site.RuntimeGID);e!=nil{fail(e);return}
 }
 if err=chownSiteTree(stage,site,a.system.webUser());err!=nil{fail(err);return}
 // Rename rather than delete: previous files remain available for recovery.
 previous:=stage+"-previous";if err=os.Rename(site.Root,previous);err!=nil{fail(err);return};if err=os.Rename(stage,site.Root);err!=nil{_ = os.Rename(previous,site.Root);fail(err);return}
 if err=a.system.labelSiteHome(ctx,site);err!=nil{fail(err);return}
 if err=a.store.UpdateSite(site.ID,func(value *Site){if database!=nil{value.Database=database.Name;value.DBUser=database.Username};value.AppType=app});err!=nil{fail(err);return}
 site.AppType=app
 if app!="regular"{if err=a.system.SetCron(site,true);err!=nil{fail(err);return};_ = a.store.UpdateSite(site.ID,func(value *Site){value.Cron=true})}
 code,e:=runCommand(ctx,30*time.Second,"curl","--silent","--output","/dev/null","--write-out","%{http_code}","--max-time","20","--header","Host: "+domain,"http://127.0.0.1/")
 job.Status="restored-review";job.Message="Website files and database restored. Local HTTP status: "+strings.TrimSpace(code)+". Verify application pages before DNS cutover. Mail, DNS records, SSL and custom cron jobs still require separate review. Previous files retained at "+previous
 if e!=nil||strings.TrimSpace(code)=="000"||strings.HasPrefix(strings.TrimSpace(code),"5")||strings.HasPrefix(strings.TrimSpace(code),"4"){job.Message="Website restored but local health check failed. DNS must not be changed. Previous files retained at "+previous}
 _ = a.saveLiveJob(job);a.store.AddActivity("Automatic website restore",domain+": application verification required","info")
}
