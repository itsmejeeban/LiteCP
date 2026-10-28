package main

import (
 "fmt"
 "net/http"
 "strconv"
 "strings"
 "time"
)

func validBackupHostProbe(host string,port int)bool{
 return port>0&&port<=65535&&len(host)>0&&len(host)<=253&&!strings.HasPrefix(host,"-")&&!strings.ContainsAny(host," /\\\r\n\t\x00")
}

func(a *App) backupHostKey(w http.ResponseWriter,r *http.Request){
 var input struct{Host string `json:"host"`;Port int `json:"port"`}
 if err:=decodeJSON(r,&input);err!=nil{writeError(w,400,err.Error());return}
 input.Host=strings.TrimSpace(input.Host)
 if !validBackupHostProbe(input.Host,input.Port){writeError(w,400,"Enter a valid SFTP host and port.");return}
 if a.system.demo{writeError(w,400,"Host-key detection requires the installed server.");return}
 scan,err:=runCommand(r.Context(),15*time.Second,"ssh-keyscan","-T","10","-p",strconv.Itoa(input.Port),"-t","ed25519,ecdsa,rsa",input.Host)
 if err!=nil{writeError(w,502,"Cannot detect the SFTP host key. Check the hostname and SSH port.");return}
 fingerprint,err:=discoveredLiveFingerprint(scan);if err!=nil{writeError(w,502,fmt.Sprintf("Cannot read the SFTP host key: %v",err));return}
 writeJSON(w,200,map[string]string{"fingerprint":fingerprint})
}
