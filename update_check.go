package main

import (
	"crypto/sha256"
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
	Build     string `json:"build,omitempty"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Checked   bool   `json:"checked"`
	Message   string `json:"message,omitempty"`
	URL       string `json:"url,omitempty"`
}

var releaseCache struct {
	sync.Mutex
	until      time.Time
	tag        string
	checked    bool
	message    string
	credential [32]byte
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
	return lookupRelease(current, false)
}

func lookupRelease(current string, refresh bool) releaseStatus {
	token, err := readPrivateUpdateToken(privateUpdateTokenPath)
	if err != nil {
		return releaseStatus{Current: current, Build: buildRevision, Message: err.Error()}
	}
	return lookupReleaseWithToken(current, token, refresh)
}

func checkReleaseWithToken(current, token string) releaseStatus {
	return lookupReleaseWithToken(current, token, false)
}

func lookupReleaseWithToken(current, token string, refresh bool) releaseStatus {
	releaseCache.Lock()
	defer releaseCache.Unlock()
	credential := sha256.Sum256([]byte(token))
	if refresh || credential != releaseCache.credential || time.Now().After(releaseCache.until) {
		releaseCache.credential = credential
		releaseCache.checked = false
		releaseCache.tag = ""
		releaseCache.message = "The release service is unavailable. Try again later."
		releaseCache.until = time.Now().Add(5 * time.Minute)
		client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/itsmejeeban/LiteCP/releases/latest", nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "LiteCP-update-check")
		if refresh {
			req.Header.Set("Cache-Control", "no-cache")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(req)
		if err == nil {
			defer response.Body.Close()
			if response.StatusCode == http.StatusNotFound {
				releaseCache.message = "No stable release is accessible. If the repository is private, configure its read-only update token on this server."
				if token != "" {
					releaseCache.message = "The update token cannot access a stable LiteCP release. Check repository selection, Contents read permission and published releases."
				}
			}
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
				releaseCache.message = "GitHub rejected the update request. Check token expiry, repository access, Contents read permission and API rate limits."
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
				releaseCache.message = ""
				releaseCache.until = time.Now().Add(time.Hour)
			}
		}
	}
	status := releaseStatus{Current: current, Build: buildRevision, Latest: releaseCache.tag, Checked: releaseCache.checked, Message: releaseCache.message, Available: newerRelease(releaseCache.tag, current)}
	if status.Available {
		status.URL = fmt.Sprintf("https://github.com/itsmejeeban/LiteCP/releases/tag/%s", releaseCache.tag)
	}
	return status
}
