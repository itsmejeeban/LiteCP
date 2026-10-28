package main

import "testing"

func TestInstallationTimezoneDefault(t *testing.T) {
 store,err:=OpenStore(t.TempDir());if err!=nil{t.Fatal(err)}
 if err=store.Initialize("admin","test-only-long-password");err!=nil{t.Fatal(err)}
 if store.Settings().Timezone!="Asia/Kolkata"{t.Fatal("incorrect installation time zone")}
 if err=store.UpdateSettings(func(settings *Settings){settings.Timezone="Europe/London"});err!=nil{t.Fatal(err)}
 reopened,err:=OpenStore(store.path[:len(store.path)-len("/config.json")]);if err!=nil{t.Fatal(err)}
 if reopened.Settings().Timezone!="Europe/London"{t.Fatal("saved time zone replaced")}
}
