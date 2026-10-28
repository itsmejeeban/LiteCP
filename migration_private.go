package main
import("context";_ "embed";"path/filepath";"strconv";"time")
//go:embed scripts/migrate-whmcs-private.py
var migrateWHMCSPrivate string
func(a *App) restoreWHMCSPrivate(ctx context.Context,jobID string,site Site,config string)error{
 root,err:=a.liveJobRoot(jobID);if err!=nil{return err};_,err=runCommand(ctx,5*time.Minute,"python3","-c",migrateWHMCSPrivate,filepath.Join(root,"native-backup"),filepath.Dir(site.Root),config,strconv.Itoa(site.RuntimeUID),strconv.Itoa(site.RuntimeGID));return err
}
