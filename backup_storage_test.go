package main

import "testing"

func TestBackupStorageCapacity(t *testing.T) {
	s, err := parseBackupStorage("sftp> df \"/backup\"\n Size Used Avail (root) %Capacity\n 1000 200 750 800 20%\n")
	if err != nil || !s.Available || s.Total != 1024000 || s.Free != 768000 || s.FreePercent != 75 {
		t.Fatalf("%+v %v", s, err)
	}
	for _, out := range []string{"", "1000 200 750 800 20%", "Size Used Avail (root) %Capacity\n0 0 0 0 0%", "Size Used Avail (root) %Capacity\n100 1 101 99 1%", "Size Used Avail (root) %Capacity\n18446744073709551615 1 1 1 1%", "Size Used Avail (root) %Capacity\n100 -1 90 90 1%"} {
		if _, err := parseBackupStorage(out); err == nil {
			t.Fatalf("accepted malformed capacity %q", out)
		}
	}
}
