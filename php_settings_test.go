package main

import (
 "strings"
 "testing"
)

func TestPHPSettingsValidationAndPoolScope(t *testing.T) {
 for _,settings:=range []map[string]string{
  {"open_basedir":"/"}, {"memory_limit":"256M\nphp_admin_value[open_basedir] = /"},
  {"upload_max_filesize":"512M","post_max_size":"128M"}, {"display_errors":"anything"},
  {"max_execution_time":"-1"}, {"date.timezone":"invalid/zone"},
 } {if validatePHPSettings(settings)==nil {t.Fatalf("accepted unsafe settings: %v",settings)}}
 settings:=map[string]string{"memory_limit":"512M","date.timezone":"Asia/Kolkata","display_errors":"Off"}
 if err:=validatePHPSettings(settings);err!=nil {t.Fatal(err)}
 output:=phpPoolSettings(settings)
 if !strings.Contains(output,"php_admin_value[memory_limit] = 512M\n") || strings.Contains(output,"open_basedir") {t.Fatal(output)}
 if phpPoolSettings(nil)!="" {t.Fatal("existing websites must retain their pool configuration")}
}
