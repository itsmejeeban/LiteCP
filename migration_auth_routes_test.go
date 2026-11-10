package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationAuthRoutes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "auth"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"login", "callback", "logout"} {
		if err := os.WriteFile(filepath.Join(root, "auth", name+".php"), []byte("<?php"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rules := `RewriteEngine On
RewriteCond %{HTTPS} !=on
RewriteRule ^ https://example.test%{REQUEST_URI} [R=301,L,NE]
RewriteRule ^auth/login/?$ auth/login.php [L,QSA]
RewriteRule ^auth/callback/?$ auth/callback.php [L,QSA]
RewriteCond %{REMOTE_ADDR} ^127\.
RewriteRule ^auth/logout/?$ auth/logout.php [L,QSA]
<If "true">
RewriteRule ^auth/logout/?$ auth/logout.php [L]
</If>
RewriteRule ^auth/login/?$ https://other.test/ [R,L]
`
	if err := os.WriteFile(filepath.Join(root, ".htaccess"), []byte(rules), 0600); err != nil {
		t.Fatal(err)
	}
	routes := migrationAuthRoutes(root)
	if !reflect.DeepEqual(routes, []string{"login", "callback"}) {
		t.Fatalf("expected only unconditional routes with matching handlers: %v", routes)
	}
	block := migrationAuthRouteBlock(routes)
	for _, route := range routes {
		if !strings.Contains(block, "location = /auth/"+route+" { rewrite ^ /auth/"+route+".php last; }") ||
			!strings.Contains(block, "location = /auth/"+route+"/") {
			t.Fatal("missing internal rewrite")
		}
	}
	if strings.Contains(block, "logout") || strings.Contains(block, "other.test") || managedMigrationAuth.ReplaceAllString("server {"+block+"\n}", "") != "server {\n}" {
		t.Fatal("unsafe or irreversible routing")
	}
}

func TestMigrationAuthRoutesNginxParser(t *testing.T) {
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("Nginx is not installed")
	}
	dir := t.TempDir()
	for _, name := range []string{"logs", "temp"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(dir, "nginx.conf")
	content := "pid nginx.pid;\nerror_log stderr;\nevents {}\nhttp {\n access_log off;\nserver {\n listen 127.0.0.1:18080;\n" +
		migrationAuthRouteBlock([]string{"login", "callback", "logout"}) + "\n location ~ \\.php$ { return 200 $args; }\n}\n}\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(nginx, "-t", "-p", dir, "-c", config).CombinedOutput(); err != nil {
		t.Fatalf("Nginx rejected the auth routes: %v\n%s", err, output)
	}
}
