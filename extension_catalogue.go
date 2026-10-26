package main
import("context";"errors";"fmt";"regexp";"sort";"strings";"time")
type ExtensionCatalogueItem struct{Key string `json:"key"`;Label string `json:"label"`;Version string `json:"version"`;Installed bool `json:"installed"`;ReadOnly bool `json:"readOnly"`;Recommended bool `json:"recommended"`;Package string `json:"package"`;Note string `json:"note"`}
var extensionPackageName=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.+-]*$`)
func extensionPackageSuffix(version,name,osID string)(string,bool){prefix:="php"+strings.ReplaceAll(version,".","")+"-php-";if osID=="ubuntu"||osID=="debian"{prefix="php"+version+"-"};if !strings.HasPrefix(name,prefix)||!extensionPackageName.MatchString(name){return "",false};suffix:=strings.TrimPrefix(name,prefix);for _,excluded:=range []string{"cli","fpm","cgi","common","devel","dev","dbg","phpdbg","embed","embedded","pear","process","dba"}{if suffix==excluded{return "",false}};if strings.HasSuffix(suffix,"-devel")||strings.HasSuffix(suffix,"-dev")||strings.HasSuffix(suffix,"-debuginfo")||strings.HasSuffix(suffix,"-debugsource"){return "",false};return suffix,suffix!=""}
func extensionRecommended(suffix string)bool{for _,key:=range []string{"mysql","mysqlnd","curl","mbstring","xml","gd","bcmath","gmp","intl","soap","imap","pecl-imap","zip","pecl-zip","opcache","pdo","ioncube-loader"}{if suffix==key{return true}};return false}
func(s *System) extensionCatalogue(ctx context.Context)([]ExtensionCatalogueItem,error){
 result:=[]ExtensionCatalogueItem{};versions:=s.installedPHPVersions();if s.demo{return result,nil}
 var available,installed string;var err error
 if s.os.ID=="ubuntu"||s.os.ID=="debian"{available,err=runCommand(ctx,60*time.Second,"apt-cache","pkgnames");if err!=nil{return nil,err};installed,err=runCommand(ctx,30*time.Second,"dpkg-query","-W","-f=${Package} ${db:Status-Status}\\n");if err!=nil{return nil,err}}else{available,err=runHostCommand(ctx,90*time.Second,"dnf","-q","repoquery","--cacheonly","--available","--qf","%{name}","php*-php-*");if err!=nil{return nil,fmt.Errorf("cannot read PHP extension repository catalogue: %w",err)};installed,err=runHostCommand(ctx,30*time.Second,"rpm","-qa","--qf","%{NAME}\\n");if err!=nil{return nil,err}}
 installedSet:=map[string]bool{};for _,line:=range strings.Split(installed,"\n"){fields:=strings.Fields(line);if len(fields)==1||len(fields)==2&&fields[1]=="installed"{if len(fields)>0{installedSet[fields[0]]=true}}}
 names:=map[string]bool{};for _,name:=range strings.Fields(available){names[name]=true};for name:=range installedSet{names[name]=true}
 for _,version:=range versions{
  modules,err:=s.phpModules(ctx,version);if err!=nil{return nil,err}
  for _,module:=range strings.Split(modules,"\n"){module=strings.TrimSpace(module);if module==""||strings.HasPrefix(module,"["){continue};result=append(result,ExtensionCatalogueItem{Key:"builtin:"+module,Label:module,Version:version,Installed:true,ReadOnly:true,Note:"Loaded module — managed by its runtime/package"})}
  if s.usesPHP73Container(version){continue}
  for name:=range names{suffix,ok:=extensionPackageSuffix(version,name,s.os.ID);if !ok{continue};recommended:=extensionRecommended(suffix);note:="Optional — install only when your application requires it";if recommended{note="WordPress / WHMCS baseline or common application support"};result=append(result,ExtensionCatalogueItem{Key:"package:"+name,Label:suffix,Version:version,Package:name,Installed:installedSet[name],ReadOnly:recommended&&installedSet[name],Recommended:recommended,Note:note})}
 }
 sort.Slice(result,func(i,j int)bool{if result[i].Version!=result[j].Version{return result[i].Version<result[j].Version};return result[i].Label<result[j].Label});return result,nil
}
func(s *System) setCatalogueExtension(ctx context.Context,version,key string,install bool)error{
 if !containsString(supportedPHPVersions,version)||!s.phpRuntimeAvailable(version)||s.usesPHP73Container(version){return errors.New("select an installed native PHP runtime")}
 name:=strings.TrimPrefix(key,"package:");if _,ok:=extensionPackageSuffix(version,name,s.os.ID);!ok{return errors.New("invalid PHP extension package")}
 catalogue,err:=s.extensionCatalogue(ctx);if err!=nil{return err};found:=false;for _,item:=range catalogue{if item.Version==version&&item.Key==key{if item.ReadOnly&&!install{return errors.New("application baseline packages are protected")};found=true;break}};if !found{return errors.New("package is absent from the configured repository catalogue")}
 wasInstalled:=s.packageInstalled(ctx,name);if wasInstalled==install{return nil}

 change:=func(changeCtx context.Context,wanted bool)error{
  args:=extensionPackageCommand(s.os.ID,name,wanted)
  _,err:=runHostCommand(changeCtx,10*time.Minute,args[0],args[1:]...);return err
 }
 restart:=func(restartCtx context.Context)error{_,err:=runCommand(restartCtx,45*time.Second,"systemctl","restart",s.phpUnitForVersion(version));return err}
 return extensionPackageTransaction(ctx,install,wasInstalled,change,restart,func(checkCtx context.Context)bool{return s.packageInstalled(checkCtx,name)})
}

func extensionPackageCommand(osID,name string,install bool)[]string{
 if osID=="ubuntu"||osID=="debian"{if install{return []string{"apt-get","install","--no-remove","--no-install-recommends","-y",name}};return []string{"dpkg","--remove",name}}
 if install{return []string{"dnf","install","--setopt=install_weak_deps=False","-y",name}};return []string{"rpm","-e",name}
}
// Restore the previously requested package state if a completed package change
// leaves PHP unable to restart. Never touch unrelated pre-existing packages.
func extensionPackageTransaction(ctx context.Context,wanted,previous bool,change func(context.Context,bool)error,restart func(context.Context)error,installed func(context.Context)bool)error{
 if wanted==previous{return nil}
 if err:=change(ctx,wanted);err!=nil{return err}
 var failure error
 if installed(ctx)!=wanted{failure=errors.New("package state verification failed")}else{failure=restart(ctx)}
 if failure==nil{return nil}
 rollbackCtx,cancel:=context.WithTimeout(context.Background(),11*time.Minute);defer cancel()
 if err:=change(rollbackCtx,previous);err!=nil{return fmt.Errorf("extension change failed: %v; previous package state could not be restored: %w",failure,err)}
 if installed(rollbackCtx)!=previous{return fmt.Errorf("extension change failed: %v; rollback package verification failed",failure)}
 if err:=restart(rollbackCtx);err!=nil{return fmt.Errorf("extension change failed: %v; package restored but PHP restart still failed: %w",failure,err)}
 return fmt.Errorf("extension change failed: %v; previous package state and PHP service restored",failure)
}
