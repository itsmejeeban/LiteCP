package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Syntax verification uses disposable files and never starts/stops a service.
func TestResourceNativeInstalledParsers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("installed Linux Nginx/FPM required")
	}
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("Nginx is not installed")
	}
	s := NewSystem(t.TempDir())
	p := nativeResourcePlan()
	fpm := ""
	for _, version := range s.installedPHPVersions() {
		if version == "7.3" {
			continue
		}
		p.Site.PHPVersion = version
		candidate := resourcePHPExecutable(s, p.Site)
		if _, e := os.Stat(candidate); e == nil {
			fpm = candidate
			break
		}
	}
	if fpm == "" {
		t.Skip("native FPM is not installed")
	}
	dir := t.TempDir()
	p.Site.Root = filepath.Join(dir, "public_html")
	if err = os.Mkdir(p.Site.Root, 0755); err != nil {
		t.Fatal(err)
	}
	php, web, _ := resourceNativeConfigsAt(s, p, dir)
	for name, content := range map[string]string{"php.conf": php, "nginx.conf": web} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range [][]string{{fpm, "-t", "-y", filepath.Join(dir, "php.conf")}, {nginx, "-t", "-c", filepath.Join(dir, "nginx.conf"), "-p", dir}} {
		output, e := exec.Command(command[0], command[1:]...).CombinedOutput()
		if e != nil {
			t.Fatalf("isolated runtime parser: %v\n%s", e, output)
		}
		t.Logf("%s: %s", command[0], output)
	}
}
