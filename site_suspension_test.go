package main
import("strings";"testing")
func TestSuspensionConfigRoundTrip(t *testing.T){ original:="server {\n listen 80;\n}\nserver {\n listen 443 ssl;\n ssl_certificate /cert;\n}\n"; blocked:=suspensionConfig(original,true);if strings.Count(blocked,"return 503")!=2{t.Fatal("both listeners must be suspended")};if suspensionConfig(blocked,true)!=blocked{t.Fatal("not idempotent")};if suspensionConfig(blocked,false)!=original{t.Fatal("unsuspension changed original configuration")}}
