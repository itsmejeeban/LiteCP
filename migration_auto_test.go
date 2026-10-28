package main

import (
	"strings"
	"testing";"os";"path/filepath"
)

func TestMigrationConfigRewrite(t *testing.T) {
	input := `<?php define('DB_NAME','source_db');define('DB_USER','old');define('DB_PASSWORD','old');define('DB_HOST','old');`
	name, err := migrationDBName(input, "wordpress")
	if err != nil || name != "source_db" {
		t.Fatal(name, err)
	}
	result, err := rewriteMigrationDB(input, "wordpress", DBCredentials{Name: "target", Username: "user", Password: "secret'$value"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "target") || strings.Contains(result, "'old'") {
		t.Fatal("configuration not rewritten")
	}
	if _, err = rewriteMigrationDB("<?php define('DB_NAME',getenv('DB'));", "wordpress", DBCredentials{}); err == nil {
		t.Fatal("dynamic config accepted")
	}
}

func TestAutoMigrationExistingDomainWaits(t *testing.T){
 dir:=t.TempDir();store,err:=OpenStore(dir);if err!=nil{t.Fatal(err)}
 site:=Site{ID:"existing",Domain:"existing.example.test",Root:filepath.Join(dir,"website")}
 if err=os.Mkdir(site.Root,0750);err!=nil{t.Fatal(err)}
 if err=os.WriteFile(filepath.Join(site.Root,"sentinel"),[]byte("preserve"),0600);err!=nil{t.Fatal(err)}
 if err=store.AddSiteWithDatabase(site,nil);err!=nil{t.Fatal(err)}
 app:=&App{store:store,system:NewSystem(dir)}
 job:=LiveMigrationJob{ID:"fixturejob1234567890",Account:"account",Accounts:[]LiveAccount{{ID:"account",Domain:site.Domain}},Status:"review"}
 root,_:=app.liveJobRoot(job.ID);if err=os.MkdirAll(root,0700);err!=nil{t.Fatal(err)}
 app.performAutoMigration(&job,false)
 if job.Status!="awaiting-overwrite"{t.Fatal(job.Status)}
 content,err:=os.ReadFile(filepath.Join(site.Root,"sentinel"));if err!=nil||string(content)!="preserve"{t.Fatal("existing site changed")}
}
