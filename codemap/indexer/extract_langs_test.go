package indexer

import (
	"io/fs"
	"path/filepath"
	"sort"
	"testing"
)

// fixture extracts a testdata repo with the given extractor and returns its
// node keys and edges ("from -> to" -> kind).
func fixture(t *testing.T, dir string, extract func(repo, root string, tracked []string, g *Graph)) (map[string]*Node, map[string]string) {
	t.Helper()
	root, _ := filepath.Abs(filepath.Join("testdata", dir))
	var tracked []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			tracked = append(tracked, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(tracked)
	g := &Graph{}
	extract("ws", root, tracked, g)
	nodes := map[string]*Node{}
	for _, n := range g.Nodes {
		nodes[n.Key] = n
	}
	edges := map[string]string{}
	for _, e := range g.Edges {
		edges[e.From+" -> "+e.To] = e.Kind
	}
	return nodes, edges
}

func checkGraph(t *testing.T, nodes map[string]*Node, edges map[string]string, wantNodes, noNodes []string, wantEdges map[string]string, noEdges []string) {
	t.Helper()
	for _, k := range wantNodes {
		if nodes[k] == nil {
			t.Errorf("missing node %s", k)
		}
	}
	for _, k := range noNodes {
		if nodes[k] != nil {
			t.Errorf("unexpected node %s", k)
		}
	}
	for k, kind := range wantEdges {
		if got, ok := edges[k]; !ok || got != kind {
			t.Errorf("edge %s: present=%v kind=%q", k, ok, got)
		}
	}
	for _, k := range noEdges {
		if _, ok := edges[k]; ok {
			t.Errorf("unexpected edge %s", k)
		}
	}
	if t.Failed() {
		var ks []string
		for k := range edges {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			t.Log(k)
		}
	}
}

func TestExtractJava(t *testing.T) {
	nodes, edges := fixture(t, "java", extractJava)
	const svc = "java:ws/src/main/java/com/acme/orders/OrderService.java:"
	const repo = "java:ws/src/main/java/com/acme/store/Repo.java:"
	const pg = "java:ws/src/main/java/com/acme/store/PgRepo.java:"
	const util = "java:ws/src/main/java/com/acme/orders/Util.java:"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService", svc + "OrderService.place", repo + "Repo.save", pg + "PgRepo.save",
			"java:ws/src/main/java/com/acme/orders/Order.java:Order"},
		[]string{"java:ws/src/test/java/com/acme/orders/OrderServiceTest.java:OrderServiceTest"},
		map[string]string{
			svc + "OrderService.place -> " + repo + "Repo.save":                                  "", // field typed Repo
			svc + "OrderService.place -> " + svc + "OrderService.validate":                       "", // unqualified call
			svc + "OrderService.place -> " + pg + "PgRepo.table":                                 "", // static call
			svc + "OrderService.place -> " + util + "Util.clean":                                 "", // static import
			svc + "OrderService.place -> java:ws/src/main/java/com/acme/orders/Order.java:Order": "", // same package
			svc + "OrderService.OrderService -> " + pg + "PgRepo":                                "", // new
			pg + "PgRepo -> " + repo + "Repo":                                                    "", // implements
			repo + "Repo.save -> " + pg + "PgRepo.save":                                          "impl",
		}, nil)
}

func TestExtractPy(t *testing.T) {
	nodes, edges := fixture(t, "py", extractPy)
	const orders = "py:ws/src/shop/orders.py:"
	const repo = "py:ws/src/shop/store/repo.py:"
	checkGraph(t, nodes, edges,
		[]string{orders + "OrderService", orders + "OrderService.place", orders + "main", repo + "TABLE", repo + "PgRepo.save"},
		[]string{"py:ws/tests/test_orders.py:<module>"},
		map[string]string{
			orders + "OrderService.place -> " + repo + "Repo.save":               "", // self.repo annotated Repo, via package re-export
			orders + "OrderService.place -> " + orders + "OrderService.validate": "", // self.method
			orders + "OrderService.place -> py:ws/src/shop/util.py:clean":        "", // import shop.util; shop.util.clean
			orders + "OrderService.__init__ -> " + repo + "PgRepo":               "",
			orders + "main -> " + orders + "OrderService.place":                  "", // svc = OrderService(...)
			repo + "PgRepo.save -> " + repo + "insert":                           "",
			repo + "PgRepo.save -> " + repo + "TABLE":                            "",
			repo + "PgRepo -> " + repo + "Repo":                                  "", // base class
			repo + "Repo.save -> " + repo + "PgRepo.save":                        "impl",
		},
		// A parameter and a local shadow the module's item and TABLE.
		[]string{repo + "insert -> " + repo + "TABLE", repo + "insert -> " + repo + "item", repo + "PgRepo.save -> " + repo + "item"})
}

func TestExtractCommonJS(t *testing.T) {
	nodes, edges := fixture(t, "cjs", extractTS)
	checkGraph(t, nodes, edges,
		[]string{"ts:ws/index.js:handler", "ts:ws/lib/store.js:save", "ts:ws/lib/db.js:Db"},
		[]string{"ts:ws/webpack.config.js:<module>"},
		map[string]string{
			"ts:ws/index.js:handler -> ts:ws/lib/store.js:save": "", // destructured require and store.save
			"ts:ws/lib/store.js:save -> ts:ws/lib/db.js:Db":     "", // module.exports = Db
		}, nil)
}

func TestExtractCS(t *testing.T) {
	nodes, edges := fixture(t, "cs", extractCS)
	const svc = "cs:ws/src/Acme.Orders/OrderService.cs:"
	const audit = "cs:ws/src/Acme.Orders/OrderService.Audit.cs:"
	const repo = "cs:ws/src/Acme.Store/IRepo.cs:"
	const pg = "cs:ws/src/Acme.Store/PgRepo.cs:"
	const util = "cs:ws/src/Acme.Orders/Util.cs:"
	const order = "cs:ws/src/Acme.Orders/Order.cs:Order"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService", svc + "OrderService.Place", svc + "OrderService.Count", audit + "OrderService.Audit",
			repo + "IRepo.Save", pg + "PgRepo.Save", order},
		[]string{"cs:ws/tests/Acme.Orders.Tests/OrderServiceTests.cs:OrderServiceTests"},
		map[string]string{
			svc + "OrderService.Place -> " + repo + "IRepo.Save":           "", // field typed IRepo
			svc + "OrderService.Place -> " + svc + "OrderService.Validate": "", // unqualified call
			svc + "OrderService.Place -> " + audit + "OrderService.Audit":  "", // other part of a partial class
			svc + "OrderService.Place -> " + pg + "PgRepo.Table":           "", // static call
			svc + "OrderService.Place -> " + util + "Util.Clean":           "", // using static
			svc + "OrderService.Place -> " + util + "Util.Shout":           "", // extension method
			svc + "OrderService.Place -> " + order:                         "", // same namespace
			svc + "OrderService.Count -> " + repo + "IRepo.Count":          "", // property through a field
			audit + "OrderService.Audit -> " + svc + "OrderService.Count":  "", // unqualified property
			svc + "OrderService.OrderService -> " + pg + "PgRepo":          "", // new
			pg + "PgRepo.Save -> " + pg + "PgRepo.Insert":                  "",
			pg + "PgRepo -> " + repo + "IRepo":                             "", // implements
			repo + "IRepo.Save -> " + pg + "PgRepo.Save":                   "impl",
			repo + "IRepo.Count -> " + pg + "PgRepo.Count":                 "impl",
		}, nil)
}

func TestExtractRust(t *testing.T) {
	nodes, edges := fixture(t, "rs", extractRust)
	const svc = "rs:ws/orders/src/service.rs:"
	const order = "rs:ws/orders/src/order.rs:"
	const util = "rs:ws/orders/src/util.rs:"
	const repo = "rs:ws/store/src/lib.rs:"
	const pg = "rs:ws/store/src/pg.rs:"
	checkGraph(t, nodes, edges,
		[]string{svc + "OrderService", svc + "OrderService::place", svc + "run", repo + "Repo::save", repo + "Repo::count",
			pg + "PgRepo::save", order + "Order::new", util + "audit"},
		[]string{svc + "unit", svc + "unit::works", "rs:ws/orders/src/tests.rs:places", "rs:ws/orders/src/service_tests.rs:places",
			"rs:ws/orders/tests/it.rs:end_to_end"},
		map[string]string{
			svc + "OrderService::place -> " + repo + "Repo::save":            "", // self.repo: Box<dyn Repo>, from another crate
			svc + "OrderService::place -> " + svc + "OrderService::validate": "", // self.method()
			svc + "OrderService::place -> " + pg + "PgRepo::table":           "", // Type::function()
			svc + "OrderService::place -> " + util + "clean":                 "", // use super::util; util::clean
			svc + "OrderService::place -> " + util + "audit":                 "", // macro_rules
			svc + "OrderService::place -> " + order + "State":                "", // glob import, enum variant
			svc + "OrderService::place -> " + order + "Order":                "", // parameter type
			svc + "OrderService::new -> " + pg + "PgRepo::new":               "",
			svc + "OrderService -> " + repo + "Repo":                         "", // field type
			svc + "run -> " + svc + "OrderService::place":                    "", // let svc = OrderService::new() returns Self
			svc + "run -> " + order + "Order::new":                           "",
			pg + "PgRepo::save -> " + pg + "PgRepo::insert":                  "",
			pg + "PgRepo::new -> " + repo + "TABLE":                          "", // use crate::{Repo, TABLE}
			pg + "PgRepo -> " + repo + "Repo":                                "", // impl Repo for PgRepo
			repo + "Repo::save -> " + pg + "PgRepo::save":                    "impl",
		},
		// A local shadows the function it is named after; tests call nothing.
		[]string{svc + "OrderService::place -> " + svc + "run", svc + "run -> " + util + "clean"})
}

func TestTSMembersAndJSX(t *testing.T) {
	nodes, edges := fixture(t, "tsx", extractTS)
	k := func(file, sym string) string { return "ts:ws/src/" + file + ":" + sym }
	checkGraph(t, nodes, edges,
		[]string{k("svc.ts", "Svc.run"), k("svc.ts", "Svc.store"), k("svc.ts", "Svc.constructor"), k("repo.ts", "Repo.open")},
		nil,
		map[string]string{
			k("svc.ts", "Svc") + " -> " + k("svc.ts", "Svc.run"):           "", // a class depends on its members
			k("svc.ts", "Svc.run") + " -> " + k("svc.ts", "Svc.store"):     "", // this.store
			k("svc.ts", "make") + " -> " + k("repo.ts", "Repo.open"):       "", // Repo.open through the import
			k("svc.ts", "Svc.constructor") + " -> " + k("repo.ts", "Repo"): "",
			k("svc.ts", "<module>") + " -> " + k("util.ts", "audit"):       "", // a top-level call is the module's
			k("view.tsx", "View") + " -> " + k("util.ts", "label"):         "",
		},
		[]string{
			k("svc.ts", "make") + " -> " + k("util.ts", "audit"),          // audit(make) is not part of make
			k("view.tsx", "View") + " -> " + k("svc.ts", "make"),          // JSX text is not code
			k("svc.ts", "Svc.store") + " -> " + k("repo.ts", "Repo.save"), // this.repo's type is unknown
		})
	if n := nodes[k("svc.ts", "make")]; n == nil || n.End != 16 {
		t.Errorf("make = %+v", n)
	}
	if n := nodes[k("svc.ts", "Svc")]; n == nil || n.Cyclo != 0 {
		t.Errorf("class complexity belongs to its members: %+v", n)
	}
}
