package main
import "testing"
func TestPHP85Compatibility(t *testing.T){
 for _,app:=range []string{"regular","wordpress"}{if err:=validateSiteOptions(app,"","8.5");err!=nil{t.Fatal(err)}}
 for _,release:=range []string{"8.13","9.0"}{if validateSiteOptions("whmcs",release,"8.5")==nil{t.Fatal("unsupported WHMCS runtime accepted")}}
 s:=&System{};if got:=s.nginxConfig(Site{ID:"php85",Domain:"php85.example.test",Root:"/home/litecp/php85.example.test/public_html",AppType:"regular",PHPVersion:"8.5"});got==""{t.Fatal("missing configuration")}
}
