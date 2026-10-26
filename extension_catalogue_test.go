package main
import("testing";"os";"context";"errors";"strings")
func TestExtensionCataloguePackageBoundary(t *testing.T){for _,name:=range []string{"php83-php-fpm","php83-php-devel","php84-php-pecl-redis6","php83-php-pecl-redis6;id","--evil"}{if _,ok:=extensionPackageSuffix("8.3",name,"almalinux");ok{t.Fatal("unexpected package",name)}};for _,name:=range []string{"php83-php-pecl-redis6","php83-php-gd","php83-php-pecl-imagick"}{if _,ok:=extensionPackageSuffix("8.3",name,"almalinux");!ok{t.Fatal("valid package rejected",name)}};if _,ok:=extensionPackageSuffix("8.3","php8.3-curl","ubuntu");!ok{t.Fatal("Ubuntu package rejected")};if extensionRecommended("pecl-redis6")||!extensionRecommended("mysqlnd"){t.Fatal("incorrect baseline classification")}}

func TestExtensionCatalogueLiveReadOnly(t *testing.T){if os.Getenv("LITECP_TEST_CATALOGUE")!="1"{t.Skip("live repository read-only check")};items,err:=NewSystem("/var/lib/litecp").extensionCatalogue(context.Background());if err!=nil{t.Fatal(err)};counts:=map[string]int{};packages:=0;for _,item:=range items{counts[item.Version]++;if item.Package!=""{packages++}};if packages<10{t.Fatal("repository extensions missing",packages)};t.Logf("Catalogue: %d entries, %d packages; per runtime: %v",len(items),packages,counts)}

func TestExtensionPackageRollbackAfterPHPFailure(t *testing.T){
 for _,initial:=range []bool{false,true}{t.Run(map[bool]string{false:"install",true:"remove"}[initial],func(t *testing.T){
 state:=initial;restarts:=0;changes:=[]bool{}
 err:=extensionPackageTransaction(context.Background(),!initial,initial,func(_ context.Context,next bool)error{changes=append(changes,next);state=next;return nil},func(context.Context)error{restarts++;if restarts==1{return errors.New("FPM failed")};return nil},func(context.Context)bool{return state})
 if err==nil||!strings.Contains(err.Error(),"restored")||state!=initial||restarts!=2||len(changes)!=2{t.Fatalf("rollback failed: state=%t restarts=%d changes=%v err=%v",state,restarts,changes,err)}
 })}
}
func TestExtensionPackageSuccessDoesNotRollback(t *testing.T){state:=false;changes:=0;restarts:=0;err:=extensionPackageTransaction(context.Background(),true,false,func(_ context.Context,next bool)error{changes++;state=next;return nil},func(context.Context)error{restarts++;return nil},func(context.Context)bool{return state});if err!=nil||changes!=1||restarts!=1||!state{t.Fatal("successful change incorrectly reverted")}}
func TestBaselineRemovalCannotUseLegacyAPI(t *testing.T){system:=&System{demo:true};if err:=system.SetPHPExtension(context.Background(),"8.3","bcmath",false);err==nil{t.Fatal("legacy API removed protected baseline")}}
