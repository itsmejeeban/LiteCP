package main
import("archive/zip";"context";"os";"path/filepath";"testing";"net/http/httptest")
func TestWebsiteZIPStructure(t *testing.T){
 root:=t.TempDir();if err:=os.Mkdir(filepath.Join(root,"empty"),0700);err!=nil{t.Fatal(err)};if err:=os.WriteFile(filepath.Join(root,".htaccess"),[]byte("test"),0600);err!=nil{t.Fatal(err)}
 destination:=filepath.Join(t.TempDir(),"public_html.zip");if err:=zipWebsite(context.Background(),root,destination);err!=nil{t.Fatal(err)}
 archive,err:=zip.OpenReader(destination);if err!=nil{t.Fatal(err)};defer archive.Close();found:=map[string]bool{};for _,file:=range archive.File{found[file.Name]=true};if !found["public_html/.htaccess"]||!found["public_html/empty/"]{t.Fatal(found)}
 outer:=filepath.Join(t.TempDir(),"website.zip");if err:=zipBackupParts(outer,map[string]string{"public_html.zip":destination,"databases.zip":destination});err!=nil{t.Fatal(err)};combined,err:=zip.OpenReader(outer);if err!=nil{t.Fatal(err)};defer combined.Close();if len(combined.File)!=2{t.Fatal("wrong outer structure")}
}
func TestWebsiteBackupRequiresAuthentication(t *testing.T){app:=&App{};response:=httptest.NewRecorder();app.routes().ServeHTTP(response,httptest.NewRequest("GET","/api/website-backups?siteId=x",nil));if response.Code!=401{t.Fatal(response.Code)}}
