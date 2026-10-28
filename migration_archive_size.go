package main

import (
 "context"
 "fmt"
 "strconv"
 "strings"
 "time"
)

func migrationArchiveBytes(ctx context.Context,path string)(int64,error){
 output,err:=runCommand(ctx,2*time.Minute,"python3","-c","import sys,tarfile\nn=0\nwith tarfile.open(sys.argv[1], 'r:*') as archive:\n for member in archive:\n  n+=max(0,member.size)+4096\n  if n>8*1024**3: raise ValueError('archive exceeds extraction limit')\nprint(n)",path)
 if err!=nil{return 0,fmt.Errorf("archive size inspection failed")};size,err:=strconv.ParseInt(strings.TrimSpace(output),10,64);if err!=nil||size<0{return 0,fmt.Errorf("invalid archive size")};return size,nil
}
