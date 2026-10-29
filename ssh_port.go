package main

import (
	_ "embed"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed scripts/configure-ssh-port.sh
var configureSSHPort string

func (a *App) sshPortSettings(w http.ResponseWriter,r *http.Request) {
	if a.system.demo { writeError(w,400,"SSH settings require the installed server"); return }
	if r.Method==http.MethodGet {
		output,err:=runCommand(r.Context(),10*time.Second,"systemctl","show","sshd.service","ssh.service","-p","ExecStart","--value")
		port:=0
		if err==nil { fields:=strings.Fields(output);for i,field:=range fields {if field=="-p"&&i+1<len(fields){value,_:=strconv.Atoi(strings.Trim(fields[i+1],";}"));if value>0{port=value}}} }
		if port==0 { output,err=runCommand(r.Context(),10*time.Second,"/usr/sbin/sshd","-T");if err==nil {for _,line:=range strings.Split(output,"\n"){fields:=strings.Fields(line);if len(fields)==2&&fields[0]=="port"{port,_=strconv.Atoi(fields[1]);break}}} }
		if port==0{writeError(w,500,"Cannot determine SSH port");return};writeJSON(w,200,map[string]int{"port":port});return
	}
	if r.Method!=http.MethodPut {writeError(w,405,"Use GET or PUT");return}
	var input struct {Port int `json:"port"`;Confirm bool `json:"confirm"`}
	if err:=decodeJSONWithLimit(r,&input,1024);err!=nil||!input.Confirm||input.Port<1||input.Port>65535 {writeError(w,400,"Confirm a valid SSH port from 1 to 65535");return}
	_,err:=runCommand(r.Context(),90*time.Second,"systemd-run","--quiet","--wait","--pipe","--collect","--property=RuntimeMaxSec=80","/bin/bash","-c",configureSSHPort,"litecp-ssh-port",strconv.Itoa(input.Port))
	if err!=nil {writeError(w,500,"SSH port change failed: "+err.Error());return}
	writeJSON(w,200,map[string]any{"port":input.Port,"message":"SSH port updated. Use this port for future SSH connections and your updater."})
}
