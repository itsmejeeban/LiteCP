package main
import "testing"
func TestBackupHostProbe(t *testing.T){for _,host:=range []string{"","-evil","host\ncommand","host/other","host name"}{if validBackupHostProbe(host,22){t.Fatal(host)}};if !validBackupHostProbe("backup.example.com",22)||validBackupHostProbe("backup.example.com",0){t.Fatal("host/port validation")}}
