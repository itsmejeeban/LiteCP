package main

import("context";"encoding/json";"fmt";"os";"path/filepath";"time")

func(a *App) stageRetainedCapture(job *LiveMigrationJob)error{
 if !job.NativeReady{return fmt.Errorf("verified native backup is required")}
 root,err:=a.liveJobRoot(job.ID);if err!=nil{return err};archive:=filepath.Join(root,"portable-backup")
 info,err:=os.Lstat(archive);if err!=nil||!info.Mode().IsRegular(){return fmt.Errorf("complete portable archive is unavailable")}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Minute);defer cancel()
 required,err:=migrationArchiveBytes(ctx,archive);if err!=nil{return err};available,err:=migrationFreeBytes(ctx,root);if err!=nil||available<required+(1<<30){return fmt.Errorf("insufficient disk space for archive plus safety reserve")}
 stage:=filepath.Join(a.system.dataDir,"migrations",job.ID)
 if _,err=os.Lstat(stage);!os.IsNotExist(err){return fmt.Errorf("staging already exists; review retained files before repeating")}
 if err=os.MkdirAll(filepath.Join(stage,"files"),0700);err!=nil{return err}
 preview,err:=extractMigration(ctx,archive,filepath.Join(stage,"files"),job.Source.Platform);if err!=nil{return err};preview.ID=job.ID;content,_:=json.Marshal(preview)
 if err=writeAtomic(filepath.Join(stage,"manifest.json"),content,0600);err!=nil{return err};job.Preview=&preview;job.Status="review";return a.saveLiveJob(job)
}
