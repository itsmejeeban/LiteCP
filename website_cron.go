package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type WebsiteCronJob struct {
	ID        string    `json:"id"`
	SiteID    string    `json:"siteId"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"`
	Command   string    `json:"command"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var websiteCronID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)
var cronNumber = regexp.MustCompile(`^[0-9]+$`)

func cronShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func validateCronSchedule(schedule string) error {
	fields := strings.Fields(schedule)
	if len(fields) != 5 || strings.ContainsAny(schedule, "\r\n\x00") {
		return errors.New("use five cron fields: minute, hour, day, month and weekday")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, field := range fields {
		for _, part := range strings.Split(field, ",") {
			step := strings.Split(part, "/")
			if len(step) > 2 || len(step) == 0 {
				return errors.New("invalid cron step")
			}
			if len(step) == 2 {
				n, err := strconv.Atoi(step[1])
				if !cronNumber.MatchString(step[1]) || err != nil || n < 1 || n > limits[i][1]-limits[i][0]+1 {
					return errors.New("cron step is out of range")
				}
			}
			if step[0] == "*" {
				continue
			}
			rangeParts := strings.Split(step[0], "-")
			if len(rangeParts) > 2 || (len(step) == 2 && len(rangeParts) != 2) {
				return errors.New("use steps with * or a numeric range")
			}
			previous := -1
			for _, value := range rangeParts {
				n, err := strconv.Atoi(value)
				if !cronNumber.MatchString(value) || err != nil || n < limits[i][0] || n > limits[i][1] || n < previous {
					return errors.New("cron field is out of range")
				}
				previous = n
			}
		}
	}
	return nil
}

func validateWebsiteCronJob(job WebsiteCronJob) error {
	if err := validateCronSchedule(job.Schedule); err != nil {
		return err
	}
	if len(job.Name) > 100 || strings.ContainsAny(job.Name, "\r\n\x00") {
		return errors.New("job name must be at most 100 characters without line breaks")
	}
	if strings.TrimSpace(job.Command) == "" || len(job.Command) > 4096 || strings.ContainsAny(job.Command, "\r\n\x00") {
		return errors.New("enter a single-line command of up to 4096 characters")
	}
	return nil
}

func (s *System) websiteCronConfigDir() string {
	if s.demo {
		return filepath.Join(s.dataDir, "cron.d")
	}
	return "/etc/cron.d"
}
func (s *System) websiteCronPath(siteID string) string {
	return filepath.Join(s.websiteCronConfigDir(), "litecp-custom-"+siteID)
}
func (s *System) websiteCronMetadataPath(siteID string) string {
	return filepath.Join(s.dataDir, "website-cron", siteID+".json")
}

func (s *System) websiteCronJobs(siteID string) ([]WebsiteCronJob, error) {
	if !websiteCronID.MatchString(siteID) {
		return nil, errors.New("invalid website identifier")
	}
	content, err := os.ReadFile(s.websiteCronMetadataPath(siteID))
	if os.IsNotExist(err) {
		return []WebsiteCronJob{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []WebsiteCronJob
	if len(content) > 1024*1024 {
		return nil, errors.New("cron metadata is too large")
	}
	if err = json.Unmarshal(content, &items); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("cron job limit exceeded")
	}
	seen := map[string]bool{}
	for _, job := range items {
		if job.SiteID != siteID || !websiteCronID.MatchString(job.ID) || seen[job.ID] {
			return nil, errors.New("invalid cron job metadata")
		}
		if err := validateWebsiteCronJob(job); err != nil {
			return nil, err
		}
		seen[job.ID] = true
	}
	if items == nil {
		items = []WebsiteCronJob{}
	}
	return items, nil
}

func (s *System) websiteCronScript(site Site, job WebsiteCronJob) (string, []byte, error) {
	if !websiteCronID.MatchString(site.ID) || !websiteCronID.MatchString(job.ID) || job.SiteID != site.ID {
		return "", nil, errors.New("invalid cron job identity")
	}
	if !filepath.IsAbs(s.dataDir) || strings.ContainsAny(s.dataDir, "%\r\n\x00") {
		return "", nil, errors.New("cron data directory must be absolute and cannot contain percent signs or line breaks")
	}
	if err := validateWebsiteCronJob(job); err != nil {
		return "", nil, err
	}
	script := []byte("export HOME=" + cronShellQuote(managedSiteRoot(site)) + "\ncd " + cronShellQuote(site.Root) + " || exit 1\n" + job.Command + "\n")
	digest := sha256.Sum256(script)
	path := filepath.Join(s.dataDir, "website-cron", "scripts", site.ID, fmt.Sprintf("%s-%x.sh", job.ID, digest))
	return path, script, nil
}

func (s *System) prepareWebsiteCronScripts(site Site, jobs []WebsiteCronJob) error {
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		path, content, err := s.websiteCronScript(site, job)
		if err != nil {
			return err
		}
		for _, dir := range []string{filepath.Join(s.dataDir, "website-cron"), filepath.Join(s.dataDir, "website-cron", "scripts"), filepath.Dir(path)} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
			info, err := os.Lstat(dir)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("cron script directory must be a real directory")
			}
			if err := os.Chmod(dir, 0700); err != nil {
				return err
			}
		}
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return errors.New("cron script must be a regular file")
			}
			existing, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(existing, content) {
				return errors.New("saved immutable cron script does not match its content hash")
			}
			if err := os.Chmod(path, 0600); err != nil {
				return err
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(content)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (s *System) renderWebsiteCron(site Site, jobs []WebsiteCronJob) ([]byte, error) {
	if !websiteCronID.MatchString(site.ID) || site.RuntimeUID < 1000 || site.RuntimeGID < 1000 || !filepath.IsAbs(site.Root) || strings.ContainsAny(site.Root, "\r\n\x00") {
		return nil, errors.New("website needs an isolated runtime identity and an absolute document root")
	}
	var out strings.Builder
	out.WriteString("# LiteCP custom website schedules. Manage through the panel.\nSHELL=/bin/sh\nPATH=/usr/local/bin:/usr/bin:/bin\nMAILTO=\"\"\n")
	for _, job := range jobs {
		if job.SiteID != site.ID || !websiteCronID.MatchString(job.ID) {
			return nil, errors.New("cron job belongs to another website")
		}
		if err := validateWebsiteCronJob(job); err != nil {
			return nil, err
		}
		if !job.Enabled {
			continue
		}
		path, _, err := s.websiteCronScript(site, job)
		if err != nil {
			return nil, err
		}
		// Root only reads immutable, private script data. The website UID's shell
		// executes it. Keeping commands out of cron also preserves '%' characters
		// and stays within Debian/Vixie cron's 1000-byte command limit.
		command := fmt.Sprintf("/bin/cat %s | /usr/bin/setpriv --reuid=%d --regid=%d --clear-groups --no-new-privs /bin/sh >/dev/null 2>&1", cronShellQuote(path), site.RuntimeUID, site.RuntimeGID)
		if len(command) >= 1000 {
			return nil, errors.New("cron script path is too long for the system scheduler")
		}
		fmt.Fprintf(&out, "# Job %s\n%s root %s\n", job.ID, strings.Join(strings.Fields(job.Schedule), " "), command)
	}
	return []byte(out.String()), nil
}

func (s *System) saveWebsiteCron(site Site, jobs []WebsiteCronJob) error {
	content, err := s.renderWebsiteCron(site, jobs)
	if err != nil {
		return err
	}
	if err := s.prepareWebsiteCronScripts(site, jobs); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.websiteCronMetadataPath(site.ID)), 0700); err != nil {
		return err
	}
	if err = os.MkdirAll(s.websiteCronConfigDir(), 0755); err != nil {
		return err
	}
	previous, err := os.ReadFile(s.websiteCronPath(site.ID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(previous) == 0 {
		previous = []byte("# No active LiteCP custom schedules.\n")
	}
	metadata, err := json.Marshal(jobs)
	if err != nil {
		return err
	}
	if err = writeAtomic(s.websiteCronPath(site.ID), content, 0600); err != nil {
		return err
	}
	if err = writeAtomic(s.websiteCronMetadataPath(site.ID), metadata, 0600); err != nil {
		if rollbackErr := writeAtomic(s.websiteCronPath(site.ID), previous, 0600); rollbackErr != nil {
			return fmt.Errorf("cron metadata save failed (%v); restoring the previous schedule also failed (%v)", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (s *System) pauseWebsiteCron(siteID string) error {
	if !websiteCronID.MatchString(siteID) {
		return errors.New("invalid website identifier")
	}
	if _, err := os.Stat(s.websiteCronPath(siteID)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return writeAtomic(s.websiteCronPath(siteID), []byte("# Website detached; custom schedules paused. Saved job definitions retained.\n"), 0600)
}

func (s *System) reconcileWebsiteCron(sites []Site) error {
	known := map[string]bool{}
	for _, site := range sites {
		known[site.ID] = true
		jobs, err := s.websiteCronJobs(site.ID)
		if err != nil {
			return fmt.Errorf("read %s schedules: %w", site.Domain, err)
		}
		if len(jobs) == 0 {
			if _, err := os.Stat(s.websiteCronPath(site.ID)); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return err
			}
		}
		content, err := s.renderWebsiteCron(site, jobs)
		if err != nil {
			return err
		}
		if err := s.prepareWebsiteCronScripts(site, jobs); err != nil {
			return err
		}
		if err = os.MkdirAll(s.websiteCronConfigDir(), 0755); err != nil {
			return err
		}
		if err = writeAtomic(s.websiteCronPath(site.ID), content, 0600); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(s.websiteCronConfigDir())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "litecp-custom-") {
			id := strings.TrimPrefix(entry.Name(), "litecp-custom-")
			if !known[id] {
				if err = s.pauseWebsiteCron(id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func serverCronTimezone() string {
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if index := strings.Index(target, "/zoneinfo/"); index >= 0 {
			return target[index+10:]
		}
	}
	zone, _ := time.Now().Zone()
	return zone
}

func (a *App) websiteCron(w http.ResponseWriter, r *http.Request) {
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	items, err := a.system.websiteCronJobs(site.ID)
	if err != nil {
		writeError(w, 500, "Cannot read this website's cron jobs")
		return
	}
	if r.Method == http.MethodGet {
		php := a.system.phpBinary(site.PHPVersion)
		if a.system.usesPHP73Container(site.PHPVersion) {
			php = ""
		} // The legacy CLI wrapper needs root container access.
		appTarget := filepath.Join(site.Root, "crons", "cron.php")
		if site.AppType == "wordpress" {
			appTarget = filepath.Join(site.Root, "wp-cron.php")
		}
		writeJSON(w, 200, map[string]any{"items": items, "domain": site.Domain, "directory": site.Root, "timezone": serverCronTimezone(), "phpCommand": php, "appCron": map[string]any{"available": site.AppType != "regular", "name": applicationName(site.AppType), "enabled": site.Cron, "schedule": "*/5 * * * *", "command": appTarget}})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		writeError(w, 405, "Use GET, POST, PUT or DELETE")
		return
	}
	var input struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Enabled  bool   `json:"enabled"`
		Confirm  bool   `json:"confirm"`
	}
	if err = decodeJSONWithLimit(r, &input, 8192); err != nil {
		writeError(w, 400, "Invalid cron job request")
		return
	}
	if r.Method != http.MethodDelete {
		if err = validateWebsiteCronJob(WebsiteCronJob{Name: input.Name, Schedule: input.Schedule, Command: input.Command}); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	job := WebsiteCronJob{SiteID: site.ID, Name: strings.TrimSpace(input.Name), Schedule: strings.Join(strings.Fields(input.Schedule), " "), Command: strings.TrimSpace(input.Command), Enabled: input.Enabled, UpdatedAt: time.Now().UTC()}
	next := append([]WebsiteCronJob{}, items...)
	action := "created"
	if r.Method == http.MethodPost {
		if len(next) >= 100 {
			writeError(w, 409, "Maximum 100 custom jobs per website")
			return
		}
		job.ID, err = randomToken(12)
		if err != nil {
			writeError(w, 500, "Cannot allocate cron job")
			return
		}
		job.CreatedAt = job.UpdatedAt
		if err = validateWebsiteCronJob(job); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		next = append(next, job)
	} else {
		index := -1
		for i, item := range next {
			if item.ID == input.ID {
				index = i
				break
			}
		}
		if index < 0 {
			writeError(w, 404, "Cron job not found for this website")
			return
		}
		if r.Method == http.MethodDelete {
			if !input.Confirm {
				writeError(w, 400, "Confirm removal of this scheduled job")
				return
			}
			action = "removed"
			next = append(next[:index], next[index+1:]...)
		} else {
			action = "updated"
			job.ID = next[index].ID
			job.CreatedAt = next[index].CreatedAt
			if err = validateWebsiteCronJob(job); err != nil {
				writeError(w, 400, err.Error())
				return
			}
			next[index] = job
		}
	}
	if err = a.system.saveWebsiteCron(*site, next); err != nil {
		writeError(w, 502, "Cannot save cron schedules: "+err.Error())
		return
	}
	a.store.AddActivity("Website cron job "+action, site.Domain+": custom schedule "+action, "success")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
