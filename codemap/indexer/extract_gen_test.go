package indexer

import (
	"testing"

	"github.com/amitbet/pr-manager/codemap/decls"
)

func TestExtractKotlin(t *testing.T) {
	nodes, edges := fixture(t, "kt", extractGenAll)
	const svc = "kt:ws/app/src/main/kotlin/acme/orders/OrderService.kt:"
	const repo = "kt:ws/app/src/main/kotlin/acme/store/Repo.kt:"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService.place", svc + "OrderService.constructor", svc + "run", repo + "PgRepo.table"},
		[]string{"kt:ws/app/src/test/kotlin/acme/orders/OrderServiceTest.kt:OrderServiceTest"},
		map[string]string{
			svc + "OrderService.place -> " + repo + "Repo.save":                               "", // constructor property typed Repo
			svc + "OrderService.place -> " + repo + "PgRepo.insert":                           "", // val pg = PgRepo()
			svc + "OrderService.place -> " + repo + "PgRepo.table":                            "", // companion object member
			svc + "OrderService.place -> " + svc + "OrderService.validate":                    "", // implicit this
			svc + "OrderService.place -> kt:ws/app/src/main/kotlin/acme/orders/Util.kt:clean": "", // wildcard import
			svc + "run -> " + svc + "OrderService.place":                                      "", // OrderService().place
			repo + "PgRepo -> " + repo + "Repo":                                               "",
			repo + "Repo.save -> " + repo + "PgRepo.save":                                     "impl",
		}, nil)
	if n := nodes[svc+"OrderService.validate"]; n.Exported || n.Kind != "kt-method" {
		t.Errorf("validate = %+v", n)
	}
}

func TestExtractRuby(t *testing.T) {
	nodes, edges := fixture(t, "rb", extractGenAll)
	const svc = "rb:ws/lib/shop/orders.rb:Shop::OrderService"
	const repo = "rb:ws/lib/shop/store/repo.rb:Shop::Store::"
	checkGraph(t, nodes, edges,
		[]string{svc, svc + ".place", svc + ".valid?", repo + "PgRepo.table", "rb:ws/lib/shop/util.rb:Shop::Util.clean"},
		[]string{"rb:ws/spec/orders_spec.rb:helper"},
		map[string]string{
			svc + ".place -> " + repo + "PgRepo.save":                     "", // @repo = repo, repo = Store::PgRepo.new
			svc + ".place -> " + repo + "PgRepo.table":                    "", // Store::PgRepo.table
			svc + ".place -> rb:ws/lib/shop/orders.rb:Shop::Audit.record": "", // @audit = Audit.new
			svc + ".place -> " + svc + ".valid?":                          "", // implicit self, name ending in ?
			svc + ".place -> rb:ws/lib/shop/util.rb:Shop::Util.clean":     "", // module method
			svc + ".initialize -> " + repo + "PgRepo":                     "", // a constant nested in the enclosing module
			"rb:ws/lib/shop/orders.rb:main -> " + svc + ".initialize":     "", // Shop::OrderService.new
			"rb:ws/lib/shop/orders.rb:main -> " + svc + ".place":          "",
			repo + "PgRepo -> " + repo + "Repo":                           "",
			repo + "Repo.save -> " + repo + "PgRepo.save":                 "impl",
		}, nil)
	if nodes[svc+".valid?"].Exported {
		t.Errorf("valid? follows private")
	}
}

func TestExtractCpp(t *testing.T) {
	nodes, edges := fixture(t, "cpp", extractGenAll)
	const svc = "cpp:ws/src/orders/service.cpp:"
	const hpp = "cpp:ws/src/store/repo.hpp:"
	const impl = "cpp:ws/src/store/repo.cpp:"
	const orderC = "c:ws/src/orders/order.c:"
	checkGraph(t, nodes, edges,
		[]string{hpp + "Repo", hpp + "Repo::save", impl + "PgRepo::save", impl + "PgRepo::table", orderC + "order_id",
			"cpp:ws/src/orders/order.h:Order"},
		[]string{"cpp:ws/tests/service_test.cpp:test_place", "cpp:ws/third_party/json/json.hpp:nlohmann::json"},
		map[string]string{
			svc + "OrderService::place -> " + hpp + "Repo::save":             "", // store::Repo* repo_
			svc + "OrderService::place -> " + impl + "PgRepo::save":          "", // store::PgRepo pg
			svc + "OrderService::place -> " + impl + "PgRepo::table":         "", // store::PgRepo::table()
			svc + "OrderService::place -> " + svc + "OrderService::validate": "",
			svc + "OrderService::validate -> " + orderC + "order_id":         "", // C function through the header
			impl + "PgRepo::save -> " + impl + "PgRepo::insert":              "", // out-of-line member, implicit this
			orderC + "order_id -> " + orderC + "checked":                     "",
			hpp + "PgRepo -> " + hpp + "Repo":                                "",
			hpp + "Repo::save -> " + impl + "PgRepo::save":                   "impl",
		},
		// A qualifier is not a call to the constructor; prototypes in the
		// class body are not uses.
		[]string{impl + "PgRepo::save -> " + impl + "PgRepo::PgRepo", hpp + "PgRepo -> " + impl + "PgRepo::save"})
	if n := nodes[orderC+"checked"]; n.Exported || n.Cyclo != 2 {
		t.Errorf("checked = %+v", n)
	}
	if nodes[svc+"OrderService::validate"].Exported {
		t.Errorf("validate is private")
	}
}

func TestExtractPHP(t *testing.T) {
	nodes, edges := fixture(t, "php", extractGenAll)
	const svc = "php:ws/src/Orders/OrderService.php:OrderService::"
	const pg = "php:ws/src/Store/PgRepo.php:PgRepo"
	checkGraph(t, nodes, edges,
		[]string{svc + "place", svc + "__construct", pg + "::table", "php:ws/src/util.php:clean"},
		[]string{"php:ws/tests/OrderServiceTest.php:OrderServiceTest", "php:ws/vendor/acme/lib.php:vendored"},
		map[string]string{
			svc + "place -> php:ws/src/Store/Repo.php:Repo::save":      "", // $this->repo, a property typed Repo
			svc + "place -> " + pg + "::table":                         "", // PgRepo::table()
			svc + "place -> " + svc + "validate":                       "", // $this->validate
			svc + "place -> php:ws/src/util.php:clean":                 "", // use function
			svc + "validate -> " + svc + "limit":                       "", // self::limit
			svc + "__construct -> " + pg:                               "", // new PgRepo
			pg + " -> php:ws/src/Store/Repo.php:Repo":                  "",
			"php:ws/src/Store/Repo.php:Repo::save -> " + pg + "::save": "impl",
		}, nil)
	if nodes[pg+"::insert"].Exported {
		t.Errorf("insert is protected")
	}
}

func TestExtractSwift(t *testing.T) {
	nodes, edges := fixture(t, "swift", extractGenAll)
	const svc = "swift:ws/Sources/Orders/OrderService.swift:"
	const repo = "swift:ws/Sources/Store/Repo.swift:"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService.place", svc + "OrderService.init", repo + "PgRepo.table", "swift:ws/Sources/Orders/Order.swift:Order.valid"},
		[]string{"swift:ws/Tests/OrdersTests/OrderServiceTests.swift:testPlace"},
		map[string]string{
			svc + "OrderService.place -> " + repo + "Repo.save":            "", // let repo: Repo, from another module
			svc + "OrderService.place -> " + repo + "PgRepo.insert":        "", // let pg = PgRepo()
			svc + "OrderService.place -> " + repo + "PgRepo.table":         "", // static computed property
			svc + "OrderService.place -> " + svc + "OrderService.validate": "",
			svc + "OrderService.init -> " + repo + "PgRepo.init":           "",
			svc + "run -> " + svc + "OrderService.init":                    "",
			repo + "Repo.save -> " + repo + "PgRepo.save":                  "impl",
		}, nil)
}

func TestExtractDart(t *testing.T) {
	nodes, edges := fixture(t, "dart", extractGenAll)
	const svc = "dart:ws/lib/src/order_service.dart:"
	const repo = "dart:ws/lib/src/store/repo.dart:"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService.place", svc + "OrderService.pg", svc + "run", repo + "PgRepo._insert"},
		[]string{"dart:ws/test/order_service_test.dart:main"},
		map[string]string{
			svc + "OrderService.place -> " + repo + "Repo.save":             "", // final Repo repo
			svc + "OrderService.place -> dart:ws/lib/src/util.dart:clean":   "", // import 'util.dart' as util
			svc + "OrderService.place -> " + svc + "OrderService._validate": "",
			svc + "OrderService.pg -> " + svc + "OrderService.OrderService": "", // factory calls the constructor
			svc + "run -> " + svc + "OrderService.pg":                       "", // named constructor
			svc + "run -> " + svc + "Order.Order":                           "", // const Order(1)
			repo + "PgRepo.save -> " + repo + "PgRepo._insert":              "",
			repo + "Repo.save -> " + repo + "PgRepo.save":                   "impl",
		}, nil)
	if nodes[repo+"PgRepo._insert"].Exported {
		t.Errorf("_insert is library-private")
	}
	if g := nodes["dart:ws/lib/src/order_service.g.dart:generated"]; g == nil || len(g.Tags) == 0 || g.Tags[0] != "generated" {
		t.Errorf("generated = %+v", g)
	}
}

func TestExtractScala(t *testing.T) {
	nodes, edges := fixture(t, "scala", extractGenAll)
	const svc = "scala:ws/src/main/scala/acme/orders/OrderService.scala:"
	const repo = "scala:ws/src/main/scala/acme/store/Repo.scala:"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService.place", svc + "OrderService.apply", svc + "Main.run"},
		[]string{"scala:ws/src/test/scala/acme/OrderServiceSpec.scala:OrderServiceSpec"},
		map[string]string{
			svc + "OrderService.place -> " + repo + "Repo.save":                                "", // class parameter repo: Repo
			svc + "OrderService.place -> " + repo + "PgRepo.insert":                            "", // val pg = new PgRepo()
			svc + "OrderService.place -> scala:ws/src/main/scala/acme/orders/Util.scala:clean": "", // import acme.util._
			svc + "OrderService.place -> " + svc + "OrderService.validate":                     "",
			svc + "Main.run -> " + svc + "OrderService.apply":                                  "", // OrderService() calls the companion's apply
			svc + "Main.run -> " + svc + "OrderService.place":                                  "",
			repo + "Repo.save -> " + repo + "PgRepo.save":                                      "impl",
		}, nil)
}

func TestExtractShell(t *testing.T) {
	nodes, edges := fixture(t, "sh", extractGenAll)
	const deploy = "sh:ws/scripts/deploy.sh:"
	const common = "sh:ws/scripts/lib/common.sh:"
	checkGraph(t, nodes, edges,
		[]string{deploy + "deploy", deploy + "main", common + "die"},
		[]string{"sh:ws/test/deploy_test.sh:test_deploy"},
		map[string]string{
			deploy + "deploy -> " + common + "die":    "", // source "$DIR/lib/common.sh"
			deploy + "deploy -> " + common + "log":    "",
			deploy + "main -> " + deploy + "deploy":   "",
			deploy + "<module> -> " + deploy + "main": "",
			common + "die -> " + common + "log":       "",
		}, nil)
	if n := nodes[deploy+"deploy"]; n.Cyclo != 3 {
		t.Errorf("deploy cyclo = %d", n.Cyclo)
	}
}

func TestExtractPowerShell(t *testing.T) {
	nodes, edges := fixture(t, "ps1", extractGenAll)
	const mod = "ps1:ws/src/Orders.psm1:"
	const repo = "ps1:ws/src/Private/Repo.ps1:"
	checkGraph(t, nodes, edges,
		[]string{mod + "Invoke-Order", mod + "Test-Order", repo + "PgRepo.Save"},
		[]string{"ps1:ws/tests/Orders.Tests.ps1:<module>"},
		map[string]string{
			mod + "Invoke-Order -> " + mod + "Test-Order":     "", // a command with a dash
			mod + "Invoke-Order -> " + repo + "PgRepo.Save":   "", // $repo = [PgRepo]::new()
			mod + "Invoke-Order -> " + repo + "PgRepo.Table":  "", // [PgRepo]::Table()
			repo + "PgRepo.Save -> " + repo + "PgRepo.Insert": "", // $this.Insert
			repo + "Repo.Save -> " + repo + "PgRepo.Save":     "impl",
		}, nil)
	if nodes[repo+"PgRepo.Insert"].Exported {
		t.Errorf("hidden members are private")
	}
}

func TestGenVendored(t *testing.T) {
	sh, kt := decls.LangFor("a.sh"), decls.LangFor("a.kt")
	for p, want := range map[string]bool{"build/deploy.sh": false, "vendor/x/y.sh": true, "app/build/tmp/A.kt": true, "src/main/kotlin/A.kt": false} {
		l := kt
		if p[len(p)-3:] == ".sh" {
			l = sh
		}
		if got := isGenVendored(p, l); got != want {
			t.Errorf("isGenVendored(%s) = %v", p, got)
		}
	}
}
