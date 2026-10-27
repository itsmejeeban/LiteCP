package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type releaseStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Checked   bool   `json:"checked"`
	URL       string `json:"url,omitempty"`
}

var releaseCache struct {
	sync.Mutex
	until   time.Time
	tag     string
	checked bool
}

func releaseNumbers(tag string) ([3]int, bool) {
	var result [3]int
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return result, false
	}
	for i, p := range parts {
		if p == "" {
			return result, false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return result, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return result, false
		}
		result[i] = n
	}
	return result, true
}

func newerRelease(latest, current string) bool {
	next, ok := releaseNumbers(latest)
	if !ok {
		return false
	}
	installed, ok := releaseNumbers(strings.SplitN(current, "-", 2)[0])
	if !ok {
		return false
	}
	for i := range next {
		if next[i] != installed[i] {
			return next[i] > installed[i]
		}
	}
	return false
}

func checkRelease(current string) releaseStatus {
	releaseCache.Lock()
	defer releaseCache.Unlock()
	if time.Now().After(releaseCache.until) {
		releaseCache.checked = false
		releaseCache.tag = ""
		releaseCache.until = time.Now().Add(5 * time.Minute)
		client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/itsmejeeban/LiteCP/releases/latest", nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "LiteCP-update-check")
		response, err := client.Do(req)
		if err == nil {
			defer response.Body.Close()
			if response.StatusCode == http.StatusNotFound {
				releaseCache.checked = true
			}
			if response.StatusCode == http.StatusOK {
				var release struct {
					Tag        string `json:"tag_name"`
					Draft      bool   `json:"draft"`
					Prerelease bool   `json:"prerelease"`
				}
				if json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(&release) == nil {
					if _, valid := releaseNumbers(release.Tag); valid && !release.Draft && !release.Prerelease {
						releaseCache.tag = release.Tag
						releaseCache.checked = true
					}
				}
			}
			if releaseCache.checked {
				releaseCache.until = time.Now().Add(time.Hour)
			}
		}
	}
	status := releaseStatus{Current: current, Latest: releaseCache.tag, Checked: releaseCache.checked, Available: newerRelease(releaseCache.tag, current)}
	if status.Available {
		status.URL = fmt.Sprintf("https://github.com/itsmejeeban/LiteCP/releases/tag/%s", releaseCache.tag)
	}
	return status
}
