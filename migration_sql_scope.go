package main

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

var dumpUseDatabase = regexp.MustCompile("(?i)^USE\\s+(?:`[^`]+`|[a-zA-Z0-9_$-]+)\\s*;\\s*$")
var dumpCreateDatabase = regexp.MustCompile("(?i)^CREATE DATABASE\\s+.*;\\s*$")

// Remap only dump preamble directives. Never rewrite table data, routines or
// later database switches: the scoped database user still rejects those.
func scopedMigrationSQL(source io.Reader, target string) *io.PipeReader {
	reader, writer := io.Pipe()
	go func() {
		input := bufio.NewReader(source)
		preamble := true
		for {
			line, err := input.ReadString('\n')
			trimmed := strings.TrimSpace(line)
			if preamble {
				switch {
				case dumpUseDatabase.MatchString(trimmed):
					line = "USE `" + target + "`;\n"
				case dumpCreateDatabase.MatchString(trimmed):
					line = "-- Source database creation omitted; LiteCP target already exists.\n"
				case trimmed == "", strings.HasPrefix(trimmed, "--"), strings.HasPrefix(trimmed, "/*"), strings.HasPrefix(strings.ToUpper(trimmed), "SET "):
				default:
					preamble = false
				}
			}
			if len(line) > 0 {
				if _, e := io.WriteString(writer, line); e != nil {
					writer.CloseWithError(e)
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					writer.Close()
				} else {
					writer.CloseWithError(err)
				}
				return
			}
		}
	}()
	return reader
}
