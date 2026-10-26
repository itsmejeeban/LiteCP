package main
import "testing"
func TestDefaultResolversExactMatch(t *testing.T){if !defaultResolversMatch([]string{"8.8.8.8","8.8.4.4"}){t.Fatal("defaults rejected")};for _,values:=range [][]string{nil,{"1.1.1.1"},{"8.8.4.4","8.8.8.8"},{"8.8.8.8","8.8.4.4","1.1.1.1"}}{if defaultResolversMatch(values){t.Fatal("changed resolvers accepted")}}}
