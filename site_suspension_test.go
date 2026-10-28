package main
import("strings";"testing")
func TestSuspensionConfigRoundTrip(t *testing.T){ original:="server {\n listen 80;\n}\nserver {\n listen 443 ssl;\n ssl_certificate /cert;\n}\n"; blocked:=suspensionConfig(original,true);if strings.Count(blocked,"return 503")!=2{t.Fatal("both listeners must be suspended")};if suspensionConfig(blocked,true)!=blocked{t.Fatal("not idempotent")};if suspensionConfig(blocked,false)!=original{t.Fatal("unsuspension changed original configuration")}}

func TestSuspensionHTTPOnlyTLSIsolation(t *testing.T){
 original:="server {\n listen 80;\n server_name example.com;\n}\n"
 site:=Site{Domain:"example.com"}
 blocked:=suspensionSiteConfig(original,site,true)
 if !strings.Contains(blocked,"ssl_reject_handshake on")||!strings.Contains(blocked,"server_name example.com;"){t.Fatal("missing domain TLS isolation")}
 if suspensionSiteConfig(blocked,site,true)!=blocked{t.Fatal("guard not idempotent")}
 if suspensionSiteConfig(blocked,site,false)!=original{t.Fatal("guard not removed on unsuspend")}
 tls:=original+"server {\n listen 443 ssl;\n ssl_certificate /cert;\n}\n"
 if strings.Contains(suspensionSiteConfig(tls,site,true),"ssl_reject_handshake"){t.Fatal("valid TLS website must retain suspension page")}
}
