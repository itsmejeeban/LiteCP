package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type smtpPortResult struct {
	Host string `json:"host"`
	Port int `json:"port"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	TCPConnected bool `json:"tcpConnected"`
	Milliseconds int64 `json:"milliseconds"`
}

func publicSMTPAddress(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func checkSMTPPort(ctx context.Context,host string,port int) smtpPortResult {
	result:=smtpPortResult{Host:host,Port:port,Status:"unreachable"}
	start:=time.Now()
	addresses,err:=net.DefaultResolver.LookupIPAddr(ctx,host)
	if err!=nil {result.Status="dns-error";result.Detail="DNS lookup failed; port blocking cannot be determined.";return result}
	var connection net.Conn
	dialer:=net.Dialer{Timeout:4*time.Second}
	for _,address:=range addresses {
		if !publicSMTPAddress(address.IP){continue}
		connection,err=dialer.DialContext(ctx,"tcp",net.JoinHostPort(address.IP.String(),strconv.Itoa(port)))
		if err==nil{break}
		if ctx.Err()!=nil{break}
	}
	result.Milliseconds=time.Since(start).Milliseconds()
	if connection==nil {
		result.Detail="Connection failed. Check local firewall, routing and the destination service."
		if errors.Is(err,syscall.ECONNREFUSED){result.Status="refused";result.Detail="Connection refused by the destination or a network firewall; provider blocking is not confirmed."}
		if ctx.Err()!=nil {result.Status="timeout";result.Detail="Timed out. Local, provider or destination filtering is possible; this does not prove provider blocking."} else if timeout,ok:=err.(net.Error);ok&&timeout.Timeout(){result.Status="timeout";result.Detail="Timed out. Filtering or a network problem is possible."}
		return result
	}
	defer connection.Close()
	result.TCPConnected=true
	_ = connection.SetDeadline(time.Now().Add(4*time.Second))
	if port==465 {
		secure:=tls.Client(connection,&tls.Config{ServerName:host,MinVersion:tls.VersionTLS12})
		if err:=secure.HandshakeContext(ctx);err!=nil{result.Status="tls-error";result.Detail="TCP connected, but TLS verification failed. The TCP port is reachable.";return result}
		connection=secure
	}
	banner,err:=bufio.NewReader(io.LimitReader(connection,4096)).ReadString('\n')
	if err==nil&&strings.HasPrefix(banner,"220") {result.Status="working";result.Detail="Connected and received an SMTP greeting.";_,_=connection.Write([]byte("QUIT\r\n"))}else{result.Status="tcp-reachable";result.Detail="TCP connected, but no normal SMTP greeting was received. The port is reachable."}
	return result
}

func (a *App) smtpPortCheck(w http.ResponseWriter,r *http.Request) {
	if a.system.demo{writeError(w,400,"SMTP checks require the installed server");return}
	ctx,cancel:=context.WithTimeout(r.Context(),12*time.Second);defer cancel()
	// Fixed public destinations prevent this diagnostic becoming an arbitrary port scanner.
	targets:=[]struct{host string;port int}{{"gmail-smtp-in.l.google.com",25},{"aspmx.l.google.com",25},{"smtp.gmail.com",465},{"smtp.mail.yahoo.com",465}}
	results:=make([]smtpPortResult,len(targets));var workers sync.WaitGroup
	for i,target:=range targets{workers.Add(1);go func(index int,host string,port int){defer workers.Done();results[index]=checkSMTPPort(ctx,host,port)}(i,target.host,target.port)}
	workers.Wait()
	writeJSON(w,200,map[string]any{"items":results,"checkedAt":time.Now().UTC(),"note":"Outbound connectivity from this server only. A timeout cannot identify who blocked the connection. Inbound access, mail delivery and IP reputation are separate checks. No email was sent."})
}
