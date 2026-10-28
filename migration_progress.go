package main

import("encoding/json";"os";"path/filepath")

type migrationTransferProgress struct{
 File string `json:"file"`
 Total int64 `json:"total"`
 Bytes int64 `json:"bytes"`
 Percent int64 `json:"percent"`
}
func(a *App) attachMigrationProgress(job *LiveMigrationJob){
 if job.Status!="running"{return};root,err:=a.liveJobRoot(job.ID);if err!=nil{return}
 content,err:=os.ReadFile(filepath.Join(root,"transfer.json"));if err!=nil{return};var progress migrationTransferProgress
 if json.Unmarshal(content,&progress)!=nil||progress.Total<=0||(progress.File!="native-backup"&&progress.File!="portable-backup"){return}
 info,err:=os.Stat(filepath.Join(root,progress.File));if err!=nil{return};progress.Bytes=info.Size();progress.Percent=progress.Bytes*100/progress.Total
 if progress.Percent>99{progress.Percent=99};job.Progress=&progress
}
