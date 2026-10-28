package main

import "testing"

func TestScanCalendar(t *testing.T) {
	for _, test := range []struct{ f, at, want string }{{"daily", "02:15", "15 2 * * * root /usr/bin/nice -n 19 /usr/local/bin/litecp scan-server\n"}, {"disabled", "02:00", ""}} {
		got, err := scanCalendar(test.f, test.at)
		if err != nil || got != test.want {
			t.Fatalf("%q %v", got, err)
		}
	}
	for _, test := range []struct{ f, at string }{{"hourly", "02:00"}, {"daily", "25:00"}, {"daily", "02:00\n* * * * * root bad"}} {
		if _, err := scanCalendar(test.f, test.at); err == nil {
			t.Fatal("unsafe schedule accepted")
		}
	}
}
