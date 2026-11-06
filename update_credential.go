package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const privateUpdateTokenPath = "/etc/litecp/github-update-token"

func readPrivateUpdateToken(path string) (string, error) {
	file, err := secureOpenManaged(filepath.Dir(path), filepath.Base(path), os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("Private update credential is unreadable; check its file ownership and permissions.")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !updateCredentialRootOwned(info) {
		return "", errors.New("Private update credential must be a root-owned regular file with mode 0600.")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return "", errors.New("Private update credential cannot be read safely.")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 20 || len(token) > 1024 {
		return "", errors.New("Private update credential has an invalid format.")
	}
	for _, c := range token {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return "", errors.New("Private update credential has an invalid format.")
		}
	}
	return token, nil
}
