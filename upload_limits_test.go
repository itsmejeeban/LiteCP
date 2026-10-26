package main
import("io";"strings";"testing")
type endlessUploadFixture struct{}
func(endlessUploadFixture)Read(buffer []byte)(int,error){clearUploadFixture(buffer);return len(buffer),nil}
func clearUploadFixture(buffer []byte){for index:=range buffer{buffer[index]=0}}
func TestUploadReaderNoSizeCap(t *testing.T){const size=int64(512<<20)+1;written,err:=io.Copy(io.Discard,managedWriteReader(io.LimitReader(endlessUploadFixture{},size),maxUploadSize));if err!=nil||written!=size{t.Fatalf("large upload reader capped: %d %v",written,err)};limited,err:=io.ReadAll(managedWriteReader(strings.NewReader("123456"),3));if err!=nil||string(limited)!="1234"{t.Fatal("bounded editor/archive writer behavior changed")}}
