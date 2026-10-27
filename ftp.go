package main

import("context";"encoding/json";"errors";"fmt";"net/http";"os";"path/filepath";"regexp";"strconv";"strings";"time")
type FTPAccount struct{ID string `json:"id"`;SiteID string `json:"siteId"`;Username string `json:"username"`;CreatedAt time.Time `json:"createdAt"`}
var ftpNamePattern=regexp.MustCompile(`^[a-z][a-z0-9_]{2,29}$`)
func validFTPPassword(password string)bool{return len(password)>=12&&len(password)<=128&&!strings.ContainsAny(password,"\r\n\x00")}
func(s *System) ftpRoot()string{return filepath.Join(s.dataDir,"ftp")}
func(s *System) ftpAccounts()([]FTPAccount,error){content,err:=os.ReadFile(filepath.Join(s.ftpRoot(),"accounts.json"));if os.IsNotExist(err){return []FTPAccount{},nil};if err!=nil{return nil,err};var accounts []FTPAccount;err=json.Unmarshal(content,&accounts);return accounts,err}
func(s *System) updateFTP(ctx context.Context,site Site,accounts []FTPAccount,account FTPAccount,password,action string)error{
 root:=s.ftpRoot();if _,err:=os.Stat(filepath.Join(root,"installed"));err!=nil{return errors.New("FTPS service is not installed; run scripts/install-ftp.sh")}
 if site.RuntimeUID<1000||site.RuntimeGID<1000{return errors.New("website must have an isolated runtime identity")}
 if !ftpNamePattern.MatchString(account.Username){return errors.New("invalid FTP username")}
 current:=filepath.Join(root,"users.passwd");original,err:=os.ReadFile(current);if err!=nil&&!os.IsNotExist(err){return err}
 previousMeta,err:=readFTPState(filepath.Join(root,"accounts.json"),[]byte("[]"));if err!=nil{return fmt.Errorf("read FTP account recovery metadata: %w",err)}
 previousDB,err:=readFTPState(filepath.Join(root,"users.pdb"),nil);if err!=nil{return fmt.Errorf("read FTP credential recovery database: %w",err)}
 work,err:=os.MkdirTemp(root,"transaction-");if err!=nil{return err}
 source:=filepath.Join(work,"users.passwd");if err=os.WriteFile(source,original,0600);err!=nil{return err}
 args:=[]string{action,account.Username,"-f",source}
 if action=="useradd"{args=append(args,"-u",strconv.Itoa(site.RuntimeUID),"-g",strconv.Itoa(site.RuntimeGID),"-d",managedSiteRoot(site),"-y","3")}
 input:="";if action!="userdel"{if !validFTPPassword(password){return errors.New("FTP password must have 12–128 characters without line breaks")};input=password+"\n"+password+"\n"}
 if _,err=runCommandInput(ctx,20*time.Second,input,"pure-pw",args...);err!=nil{return errors.New("FTP account update failed; existing accounts unchanged")}
 db:=filepath.Join(work,"users.pdb")
 if _,err=runCommand(ctx,20*time.Second,"pure-pw","mkdb",db,"-f",source);err!=nil{return errors.New("FTP credential database build failed")}
 content,err:=json.Marshal(accounts);if err!=nil{return err}
 // Preserve prior credentials and metadata for rollback. Transaction files are
 // retained root-only; no website content or OS account is removed.
 nextSource,err:=os.ReadFile(source);if err!=nil{return err};nextDB,err:=os.ReadFile(db);if err!=nil{return err}
 if err=writeAtomic(current,nextSource,0600);err!=nil{return err}
 if err=writeAtomic(filepath.Join(root,"accounts.json"),content,0600);err!=nil{_ = writeAtomic(current,original,0600);return err}
 if err=writeAtomic(filepath.Join(root,"users.pdb"),nextDB,0600);err!=nil{_ = writeAtomic(current,original,0600);_ = writeAtomic(filepath.Join(root,"accounts.json"),previousMeta,0600);if len(previousDB)>0{_ = writeAtomic(filepath.Join(root,"users.pdb"),previousDB,0600)};return err}
 return nil
}
func readFTPState(path string,missing []byte)([]byte,error){content,err:=os.ReadFile(path);if os.IsNotExist(err){return missing,nil};return content,err}
func(a *App) websiteFTP(w http.ResponseWriter,r *http.Request){
 site:=a.store.SiteByID(r.URL.Query().Get("siteId"));if site==nil{writeError(w,404,"Website not found");return}
 accounts,err:=a.system.ftpAccounts();if err!=nil{writeError(w,500,"Cannot read FTP accounts");return}
 if r.Method==http.MethodGet{items:=[]FTPAccount{};for _,account:=range accounts{if account.SiteID==site.ID{items=append(items,account)}};_,err:=os.Stat(filepath.Join(a.system.ftpRoot(),"installed"));writeJSON(w,200,map[string]any{"items":items,"available":err==nil,"host":primaryIP(),"port":21,"protocol":"Explicit FTP over TLS (FTPS)","directory":managedSiteRoot(*site)});return}
 if a.system.demo{writeError(w,400,"FTPS requires the installed Linux server");return}
 var input struct{Username,Password,ID string;Confirm bool};if decodeJSONWithLimit(r,&input,4096)!=nil{writeError(w,400,"Invalid FTP request");return}
 var account FTPAccount;action:="";next:=append([]FTPAccount{},accounts...)
 switch r.Method{
 case http.MethodPost:
  if !ftpNamePattern.MatchString(input.Username)||!validFTPPassword(input.Password){writeError(w,400,"Username: 3–30 lowercase letters, numbers or underscores, starting with a letter. Password: 12–128 characters.");return}
  if len(accounts)>=200{writeError(w,409,"FTP account limit reached");return};for _,item:=range accounts{if item.Username==input.Username{writeError(w,409,"FTP username already exists");return}}
  id,err:=randomToken(12);if err!=nil{writeError(w,500,"Cannot allocate account");return};account=FTPAccount{ID:id,SiteID:site.ID,Username:input.Username,CreatedAt:time.Now().UTC()};next=append(next,account);action="useradd"
 case http.MethodPut,http.MethodDelete:
  index:=-1;for i,item:=range accounts{if item.ID==input.ID&&item.SiteID==site.ID{account=item;index=i;break}};if index<0{writeError(w,404,"FTP account not found for this website");return}
  if r.Method==http.MethodDelete{if !input.Confirm{writeError(w,400,"Confirm FTP login removal; website files will be kept");return};next=append(next[:index],next[index+1:]...);action="userdel"}else{if !validFTPPassword(input.Password){writeError(w,400,"Enter a password of 12–128 characters");return};action="passwd"}
 default:writeError(w,405,"Use GET, POST, PUT or DELETE");return
 }
 if err=a.system.updateFTP(r.Context(),*site,next,account,input.Password,action);err!=nil{writeError(w,502,err.Error());return}
 a.store.AddActivity("Website FTP account updated",fmt.Sprintf("%s: %s (%s); website files kept",site.Domain,account.Username,action),"success")
 writeJSON(w,200,map[string]bool{"ok":true})
}
