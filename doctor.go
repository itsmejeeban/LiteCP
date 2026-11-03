package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

//go:embed scripts/litecp-doctor.sh
var doctorScript string

func runDoctor(args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--check" && args[0] != "--repair") {
		return fmt.Errorf("usage: litecp doctor [--check|--repair]")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("run litecp doctor as root or with sudo")
	}
	command := exec.Command("/usr/bin/bash", append([]string{"-s", "--"}, args...)...)
	command.Stdin = strings.NewReader(doctorScript)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
