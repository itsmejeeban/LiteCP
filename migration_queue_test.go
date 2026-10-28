package main
import "testing"
func TestSelectedMigrationAccounts(t *testing.T){
 job:=LiveMigrationJob{Accounts:[]LiveAccount{{ID:"alpha"},{ID:"beta"}}}
 got,err:=selectedMigrationAccounts(job,[]string{"alpha","beta","alpha"});if err!=nil||len(got)!=2{t.Fatal(got,err)}
 for _,ids:=range [][]string{nil,{"unknown"},{"alpha;id"}}{if _,err:=selectedMigrationAccounts(job,ids);err==nil{t.Fatal("invalid accounts accepted",ids)}}
}
