//go:build !linux

package main

import "fmt"

func resourcePrepareStorage(Site) error { return fmt.Errorf("Linux is required") }

func resourceKillCgroup(string) error { return fmt.Errorf("Linux is required") }

func resourceAdoptUID(Site) error { return fmt.Errorf("Linux is required") }

func resourceRuntimeLock(string, bool) (func(), error) { return nil, fmt.Errorf("Linux is required") }
func resourceSetOOMGroup(string) error                 { return fmt.Errorf("Linux is required") }
func resourceVerifyKernel(string, WebsiteResourceLimits) error {
	return fmt.Errorf("Linux is required")
}
func resourceLiveSample(string, int64) (resourceSample, int64, int, error) {
	return resourceSample{}, 0, 0, fmt.Errorf("Linux is required")
}
func resourceBlockDevice(Site) string        { return "" }
func resourceQuotaUsage(Site) (int64, error) { return 0, fmt.Errorf("Linux is required") }
func resourceQuotaSet(Site, int64) error     { return fmt.Errorf("Linux is required") }
func resourceQuotaPrepare(Site, int64) error { return fmt.Errorf("Linux is required") }

func resourceQuotaVerifiedUsage(Site, int64) (int64, error) {
	return 0, fmt.Errorf("Linux is required")
}
