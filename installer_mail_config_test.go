package main

import (
 "os"
 "os/exec"
 "strings"
 "testing"
)

func TestMailInstallerRejectsUnusableTLSBeforePackages(t *testing.T) {
 bash,err:=exec.LookPath("bash");if err!=nil{t.Skip("bash required")}
 data,err:=os.ReadFile("scripts/install-features.sh");if err!=nil{t.Fatal(err)};script:=strings.ReplaceAll(string(data),"\r\n","\n")
 start:=strings.Index(script,"  host=${LITECP_MAIL_HOSTNAME:");end:=strings.Index(script[start:],"  if [[ $ID == ubuntu ]]; then\n    packages")
 if start<0||end<0{t.Fatal("mail preflight section missing")};preflight:=script[start:start+end]
 prefix:="check() {\nlocal host cert key panel cert_public key_public\n"+preflight+"\n}\ncheck\n"
 for _,test:=range []struct{name,host,cert,key,body string;ok bool}{
  {"local_hostname","litecp","/cert","/key","",false},
  {"missing_certificate","mail.example.test","/missing-cert","/missing-key","",false},
  {"matching_certificate","mail.example.test","/dev/null","/dev/null","openssl() { if [[ $* == *'-pubkey'* ]]; then printf matching; elif [[ $* == *'-pubout'* ]]; then printf matching; elif [[ $* == *'-outform DER'* ]]; then cat; fi; }\n",true},
  {"key_mismatch","mail.example.test","/dev/null","/dev/null","openssl() { if [[ $* == *'-pubkey'* ]]; then printf cert; elif [[ $* == *'-pubout'* ]]; then printf key; elif [[ $* == *'-outform DER'* ]]; then cat; fi; }\n",false},
 }{t.Run(test.name,func(t *testing.T){command:=exec.Command(bash,"-e","-o","pipefail","-c",test.body+prefix);command.Env=append(os.Environ(),"LITECP_MAIL_HOSTNAME="+test.host,"LITECP_MAIL_CERT="+test.cert,"LITECP_MAIL_KEY="+test.key);output,err:=command.CombinedOutput();if (err==nil)!=test.ok{t.Fatalf("preflight ok=%t: %v %s",test.ok,err,output)}})}
}

func TestMailTrustStoreReadableWithPrivateInstallerUmask(t *testing.T){
 bash,err:=exec.LookPath("bash");if err!=nil{t.Skip("bash required")};content,err:=os.ReadFile("scripts/install-features.sh");if err!=nil{t.Fatal(err)}
 text:=strings.ReplaceAll(string(content),"\r\n","\n");start:=strings.Index(text,"  cp \"$bundle\" /opt/litecp/webmail/config/mail-ca.pem");end:=strings.Index(text[start:],"  chown root:litecp-webmail /opt/litecp/webmail/config/config.inc.php")
 if start<0||end<0{t.Fatal("mail trust store installation section missing")};section:=strings.ReplaceAll(text[start:start+end],"/opt/litecp/webmail/config/mail-ca.pem","\"$target\"")
 command:=exec.Command(bash,"-e","-c","umask 077\nchown() { :; }\nbundle=$1\ntarget=$2\n"+section+"\n[[ $(stat -c %a \"$target\") == 640 ]]\n","fixture", "/dev/null",t.TempDir()+"/mail-ca.pem")
 if output,err:=command.CombinedOutput();err!=nil{t.Fatalf("webmail trust store is unreadable with private installer umask: %v %s",err,output)}
}
