package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type panelStorageEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Category string `json:"category"`
	Bytes int64 `json:"bytes"`
	Status string `json:"status"`
	IsDir bool `json:"isDir"`
}

func (a *App) liteCPStorageUsage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	entries := []panelStorageEntry{}
	seen := map[string]bool{}
	add := func(name,path,category string) {
		path = filepath.Clean(path)
		if seen[path] { return }; seen[path] = true
		entry := panelStorageEntry{Name:name,Path:path,Category:category,Status:"unavailable"}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) { entry.Status="not-created" } else if err == nil && info.Mode()&os.ModeSymlink == 0 {
			entry.IsDir = info.IsDir()
			output, scanErr := exec.CommandContext(ctx,"du","-x","-s","-B1","--null","--",path).Output()
			fields := strings.SplitN(string(output),"\t",2)
			if len(fields)==2 { if bytes, parseErr := strconv.ParseInt(fields[0],10,64); parseErr==nil { entry.Bytes=bytes;entry.Status="measured";if scanErr!=nil{entry.Status="partial"} } }
		}
		entries = append(entries,entry)
	}
	add("Website files, retained sites and private folders",siteStorageBase,"Websites")
	add("Panel settings and data",a.system.dataDir,"Panel")
	for _, item := range []struct{name,path string}{
		{"Live migration archives and job data","live-migrations"},{"Extracted migration and restore staging","migrations"},
		{"Manual website backups","website-backups"},{"Legacy file trash","trash"},
		{"Application hosting configuration and logs","application-hosting"},{"Website cron configuration","website-cron"},
		{"FTP account configuration","ftp"},{"Mail signing keys","dkim"},{"Malware scan reports","server-scan"},
	} { add(item.name,filepath.Join(a.system.dataDir,item.path),"Panel data breakdown") }
	for _, item := range []struct{name,path,category string}{
		{"Panel executable","/usr/local/bin/litecp","Software"},
		{"Panel support scripts","/usr/local/lib/litecp","Software"},
		{"Update recovery copies and downloaded sources","/usr/local/lib/litecp-updates","Updates"},
		{"phpMyAdmin and webmail software","/opt/litecp","Software"},
		{"Server backups and system snapshots","/var/backups/litecp","Backups"},
		{"System snapshots","/var/backups/litecp/system-snapshots","Backup breakdown"},
		{"Mailboxes","/var/vmail","Mail"},
		{"Mail queue","/var/spool/postfix","Mail"},
		{"Malware scanner signatures","/var/lib/clamav","Hosting services"},
		{"Legacy website storage","/var/www/litecp","Websites"},
		{"MySQL data (shared service)","/var/lib/mysql","Hosting services"},
		{"Container images and application runtimes (shared)","/var/lib/containers","Hosting services"},
		{"Web server logs (shared)","/var/log/nginx","Logs"},
		{"Web server configuration (shared)","/etc/nginx","Configuration"},
		{"Mail server configuration","/etc/postfix","Configuration"},
		{"Mailbox server configuration","/etc/dovecot","Configuration"},
		{"Mail signing configuration","/etc/opendkim","Configuration"},
		{"FTP service configuration","/etc/vsftpd","Configuration"},
	} { add(item.name,item.path,item.category) }
	// Include private folders for retained homes as well as registered websites.
	if homes,err:=os.ReadDir(siteStorageBase);err==nil { for _,home:=range homes { if !home.IsDir(){continue}; for _,folder:=range []string{"backups",".trash","tmp","logs"} { path:=filepath.Join(siteStorageBase,home.Name(),folder);if _,err:=os.Lstat(path);err==nil{add(home.Name()+" / "+folder,path,"Website private folders")} } } }
	// User-run updater workspaces retain sources and recovery binaries here.
	if roots,err:=os.ReadDir("/root");err==nil { for _,entry:=range roots { if entry.IsDir()&&(strings.HasPrefix(entry.Name(),"litecp-manual-update-")||strings.HasPrefix(entry.Name(),"litecp-reinstall-")){add("Source and update workspace","/root/"+entry.Name(),"Updates")} } }
	for _, directory := range []string{"/usr/local/bin", "/tmp"} {
		if files,err:=os.ReadDir(directory);err==nil { for _,file:=range files { if strings.HasPrefix(file.Name(),"litecp-")||strings.HasPrefix(file.Name(),"litecp.before-"){add("Retained update or temporary artifact",filepath.Join(directory,file.Name()),"Updates and temporary files")} } }
	}
	writeJSON(w,200,map[string]any{"items":entries,"partial":ctx.Err()!=nil,"note":"Parent directories include their listed subdirectories. Sizes overlap; do not add the rows together. Shared service paths may also contain data used outside LiteCP."})
}
