package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "net"
 "net/http"
 "os"
 "path/filepath"
 "sync"
 "strings"
 "time"
)

type pmaLaunch struct { DatabaseID string `json:"-"`; Username string `json:"username"`; Password string `json:"password"`; Database string `json:"database"`; Session string `json:"-"`; Expires time.Time `json:"-"` }
var pmaTicketMu sync.Mutex
var pmaTickets=map[string]pmaLaunch{}

func (a *App) databaseAdminLaunch(w http.ResponseWriter,r *http.Request) {
 var input struct{ ID string `json:"id"` }
 if err:=decodeJSON(r,&input);err!=nil{writeError(w,400,err.Error());return}
 database:=a.store.DatabaseByID(input.ID)
 if database==nil{writeError(w,404,"Managed database not found.");return}
 if a.system.demo{writeError(w,400,"Automatic database access requires the installed server.");return}
 credentials,err:=a.system.pmaCredentials(r.Context(),*database)
 if err!=nil{writeError(w,502,"Cannot prepare database access. Verify phpMyAdmin installation and MySQL availability.");return}
 token,err:=randomToken(32);if err!=nil{writeError(w,500,"Could not start database session.");return}
 cookie,_:=r.Cookie("litecp_session")
 credentials.Session=cookie.Value;credentials.Expires=time.Now().Add(30*time.Second)
 pmaTicketMu.Lock()
 for key,value:=range pmaTickets{if time.Now().After(value.Expires){delete(pmaTickets,key)}}
 if len(pmaTickets)>=100{pmaTicketMu.Unlock();writeError(w,429,"Too many pending database sessions.");return}
 pmaTickets[token]=credentials;pmaTicketMu.Unlock()
 writeJSON(w,200,map[string]string{"ticket":token})
}

func (s *System) pmaCredentials(ctx context.Context,database ManagedDatabase)(pmaLaunch,error){
 var result pmaLaunch
 if err:=validateDatabaseObjects(database.Name,database.Username);err!=nil{return result,err}
 digest:=sha256.Sum256([]byte(database.ID+":"+database.Name))
 result.Username="lpma_"+hex.EncodeToString(digest[:])[:24];result.Database=database.Name;result.DatabaseID=database.ID
 root:=filepath.Join(s.dataDir,"phpmyadmin-credentials")
 if err:=os.MkdirAll(root,0700);err!=nil{return result,err}
 file:=filepath.Join(root,hex.EncodeToString(digest[:])+".json")
 var saved struct{ Cipher string `json:"cipher"` }
 content,err:=os.ReadFile(file)
 if err==nil {
  if err=json.Unmarshal(content,&saved);err!=nil{return result,err}
  result.Password,err=s.DecryptSecret(saved.Cipher);if err!=nil{return result,err}
 }else if os.IsNotExist(err){
  result.Password,err=randomToken(32);if err!=nil{return result,err}
  saved.Cipher,err=s.EncryptSecret(result.Password);if err!=nil{return result,err}
  content,_=json.Marshal(saved);if err=writeAtomic(file,content,0600);err!=nil{return result,err}
 }else{return result,err}
 // The internal account has access to this one managed database, with no
 // global privileges or GRANT OPTION. Application passwords are unchanged.
 pattern:=database.Name
 partial,err:=runCommandInput(ctx,5*time.Second,"SELECT @@partial_revokes;",firstAvailableCommand("mysql","mariadb"),"--protocol=socket","--user=root","--batch","--skip-column-names")
 if err!=nil{return result,err}
 switch strings.TrimSpace(partial){case "0":pattern=strings.ReplaceAll(pattern,"_",`\_`);case "1":default:return result,fmt.Errorf("unknown MySQL database grant mode")}
 sql:=fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' %s; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';",result.Username,s.databaseUserIdentification(result.Password,false),pattern,result.Username)
 _,err=runCommandInput(ctx,20*time.Second,sql,firstAvailableCommand("mysql","mariadb"),"--protocol=socket","--user=root","--batch")
 return result,err
}

func (a *App) consumePMATicket(w http.ResponseWriter,r *http.Request){
 host,_,err:=net.SplitHostPort(r.RemoteAddr)
 ip:=net.ParseIP(host)
 if err!=nil||ip==nil||!ip.IsLoopback()||r.Host!="127.0.0.1:9090"||r.Header.Get("X-Forwarded-For")!=""||r.Header.Get("X-Forwarded-Proto")!=""{writeError(w,403,"Internal access only.");return}
 var input struct{Ticket string `json:"ticket"`}
 if err:=decodeJSONWithLimit(r,&input,1024);err!=nil{writeError(w,400,"Invalid launch ticket.");return}
 pmaTicketMu.Lock();launch,ok:=pmaTickets[input.Ticket];delete(pmaTickets,input.Ticket);pmaTicketMu.Unlock()
 if !ok||time.Now().After(launch.Expires)||!a.validSession(launch.Session)||a.store.DatabaseByID(launch.DatabaseID)==nil{writeError(w,403,"Database launch expired. Open the database again from LiteCP.");return}
 writeJSON(w,200,launch)
}
