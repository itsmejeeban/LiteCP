package main
import "testing"
func TestPHPVersionSwitchAllRuntimes(t *testing.T) {
 for _,v:=range supportedPHPVersions { if err:=validatePHPVersionSwitch(v);err!=nil {t.Fatal(v,err)} }
 for _,v:=range []string{"", "8.6", "8.3;id"} {if validatePHPVersionSwitch(v)==nil {t.Fatal("invalid runtime accepted",v)}}
}
