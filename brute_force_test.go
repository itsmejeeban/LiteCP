package main

import "testing"

func TestBruteForceRuleValidation(t *testing.T) {
	valid := bruteForceRule{Jail: "sshd", MaxRetry: 5, FindTime: 600, BanTime: 3600}
	if !validBruteForceRule(valid) {
		t.Fatal("valid rule rejected")
	}
	for _, jail := range []string{"sshd;id", "DEFAULT", "../sshd", ""} {
		v := valid
		v.Jail = jail
		if validBruteForceRule(v) {
			t.Fatalf("unsafe jail accepted: %q", jail)
		}
	}
	for _, change := range []func(*bruteForceRule){func(v *bruteForceRule) { v.MaxRetry = 0 }, func(v *bruteForceRule) { v.MaxRetry = 101 }, func(v *bruteForceRule) { v.FindTime = 59 }, func(v *bruteForceRule) { v.FindTime = 86401 }, func(v *bruteForceRule) { v.BanTime = 59 }, func(v *bruteForceRule) { v.BanTime = 2592001 }} {
		v := valid
		change(&v)
		if validBruteForceRule(v) {
			t.Fatal("invalid limit accepted")
		}
	}
	for _, jail := range bruteForceJails {
		v := valid
		v.Jail = jail
		if !validBruteForceRule(v) {
			t.Fatalf("managed jail rejected: %s", jail)
		}
	}
}
