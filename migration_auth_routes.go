package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var apacheAuthRewrite = regexp.MustCompile(`^\^auth/(login|callback|logout)/\?\$[ \t]+/?auth/(login|callback|logout)\.php[ \t]+(?i:\[(?:L(?:,QSA)?|QSA,L)\])$`)
var managedMigrationAuth = regexp.MustCompile(`(?ms)\n    # BEGIN LITECP MIGRATION AUTH\n.*?^    # END LITECP MIGRATION AUTH\n`)

func migrationAuthRoutes(root string) []string {
	path := filepath.Join(root, ".htaccess")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	authDir := filepath.Join(root, "auth")
	if info, err := os.Lstat(authDir); err != nil || !info.IsDir() {
		return nil
	}
	var routes []string
	enabled, conditional := false, false
	sectionDepth := 0
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// A continued directive or an included file can change the meaning of
		// later rules. Only translate routes from the simple, top-level form.
		if strings.HasSuffix(line, `\`) {
			return nil
		}
		if strings.HasPrefix(line, "</") {
			sectionDepth--
			continue
		}
		if strings.HasPrefix(line, "<") {
			sectionDepth++
			continue
		}
		if sectionDepth != 0 {
			continue
		}
		directive := strings.Fields(line)[0]
		arguments := strings.TrimSpace(line[len(directive):])
		switch strings.ToLower(directive) {
		case "include", "includeoptional":
			return nil
		case "rewriteengine":
			enabled = strings.EqualFold(arguments, "on")
		case "rewritecond":
			conditional = true
		case "rewriterule":
			rule := apacheAuthRewrite.FindStringSubmatch(arguments)
			if !conditional && rule != nil && rule[1] == rule[2] && !containsString(routes, rule[1]) {
				if info, err := os.Lstat(filepath.Join(authDir, rule[1]+".php")); err == nil && info.Mode().IsRegular() {
					routes = append(routes, rule[1])
				}
			}
			conditional = false
		}
	}
	if !enabled || sectionDepth != 0 {
		return nil
	}
	return routes
}

func migrationAuthRouteBlock(routes []string) string {
	var block strings.Builder
	for _, route := range []string{"login", "callback", "logout"} {
		if containsString(routes, route) {
			fmt.Fprintf(&block, "    location = /auth/%[1]s { rewrite ^ /auth/%[1]s.php last; }\n    location = /auth/%[1]s/ { rewrite ^ /auth/%[1]s.php last; }\n", route)
		}
	}
	if block.Len() == 0 {
		return ""
	}
	return "\n    # BEGIN LITECP MIGRATION AUTH\n" + block.String() + "    # END LITECP MIGRATION AUTH\n"
}

func (s *System) applyMigrationAuthRoutes(ctx context.Context, site Site) error {
	if s.demo || site.AppType != "regular" || site.Application != nil || resourceEnabled(site) {
		return nil
	}
	path, _ := s.nginxPaths(site.Domain)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := managedMigrationAuth.ReplaceAllString(string(data), "")
	block := migrationAuthRouteBlock(site.ApacheAuthRoutes)
	if block != "" {
		content = sslServerStart.ReplaceAllStringFunc(content, func(start string) string { return start + block })
	}
	if content == string(data) {
		return nil
	}
	return s.writeApplicationRouting(ctx, site, []byte(content))
}
