package decls

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strings"
	"testing"
)

// genCase is a file of one generic-parser language and what it declares:
// "Name/kind" in file order ("-" marks a private one), the imports, and
// the complexity of some members (cyclo/nest).
type genCase struct {
	path, src string
	decls     string
	imports   string
	cx        map[string]string
	ns        string
}

func (c genCase) run(t *testing.T) {
	f := ParseGen(c.path, c.src)
	if f == nil {
		t.Fatalf("%s: no language", c.path)
	}
	var ds []string
	got := map[string]*GenDecl{}
	for _, d := range f.Decls {
		s := d.Name + "/" + d.Kind
		if !d.Exported {
			s = "-" + s
		}
		ds = append(ds, s)
		if got[d.Name] == nil {
			got[d.Name] = d
		}
	}
	if g := strings.Join(ds, " "); g != c.decls {
		t.Errorf("decls:\n got %s\nwant %s", g, c.decls)
	}
	var is []string
	for _, i := range f.Imports {
		s := i.Path
		switch {
		case i.File:
			s = "file:" + s
		case i.Wildcard:
			s += ".*"
		case i.Module:
			s = "module:" + s
		}
		if i.Alias != "" {
			s += " as " + i.Alias
		}
		is = append(is, s)
	}
	if g := strings.Join(is, ", "); g != c.imports {
		t.Errorf("imports = %s, want %s", g, c.imports)
	}
	for name, want := range c.cx {
		d := got[name]
		if d == nil {
			t.Errorf("no %s", name)
			continue
		}
		if g := fmt.Sprintf("%d/%d", d.Cyclo, d.Nest); g != want {
			t.Errorf("%s cx = %s, want %s", name, g, want)
		}
	}
	if g := strings.Join(f.Namespaces, ","); g != c.ns {
		t.Errorf("namespaces = %s, want %s", g, c.ns)
	}
}

func TestParseShell(t *testing.T) {
	genCase{path: "scripts/deploy.sh", src: `#!/bin/bash
source "$(dirname "$0")/lib.sh"
# Deploys.
deploy() {
  if [[ -n "$1" && -f f ]]; then
    helper "$1" | grep -q ok || exit 1
  elif true; then echo; fi
  for i in 1 2; do case "$i" in 1) echo;; *) ;; esac; done
}
function my-helper { deploy "a"; }
`,
		decls: "deploy/function my-helper/function", imports: "file:./lib.sh",
		cx: map[string]string{"deploy": "8/2"}, // if && || elif for case case
	}.run(t)
}

func TestParsePowerShell(t *testing.T) {
	genCase{path: "Orders.psm1", src: `. $PSScriptRoot\Private\Repo.ps1
function Get-Thing {
  param([string]$Name)
  if ($Name -and $x) { Invoke-Other } elseif ($y) { }
  foreach ($i in 1..3) { try { } catch { } }
}
class PgRepo : Repo {
  PgRepo() { }
  [void] Save([string]$x) { $this.Insert($x) }
  hidden [void] Insert([string]$x) { }
}
`,
		decls:   "Get-Thing/function PgRepo/class PgRepo.PgRepo/ctor PgRepo.Save/method -PgRepo.Insert/method",
		imports: "file:./Private/Repo.ps1",
		cx:      map[string]string{"Get-Thing": "6/2"}, // if -and elseif foreach catch
	}.run(t)
}

func TestParseC(t *testing.T) {
	src := `#include "store.h"
#include <stdio.h>
#define MAX(a,b) ((a)>(b)?(a):(b))
struct order { int id; };
typedef struct { int x; } point_t;
enum state { NEW, DONE };
/* Places. */
static int place(struct order *o, int n) {
  if (o && n > 0) { for (int i = 0; i < n; i++) { save(o); } }
  else if (n < 0) return -1;
  switch (n) { case 1: break; default: break; }
  return n ? 1 : 0;
}
int save(struct order *o) { return o->id; }
#ifdef X
void extra(void) {}
#endif
`
	genCase{path: "src/order.c", src: src,
		decls:   "-MAX/macro order/struct point_t/typedef state/enum -place/function save/function extra/function",
		imports: "file:store.h",
		cx:      map[string]string{"place": "7/2"}, // if && for else-if case ?: ; else if stays at depth 1
	}.run(t)
	if d := Innermost(GenDecls("src/order.c", src), 10, 10); d == nil || d.Name != "place" {
		t.Errorf("line 10 -> %v", d)
	}
}

func TestParseCpp(t *testing.T) {
	genCase{path: "src/repo.cpp", src: `#include "repo.hpp"
namespace acme { namespace store {
class PgRepo : public Repo, private Base<int> {
 public:
  PgRepo() : Repo() {}
  virtual int count() = 0;
  int save(const Order& o) override { return insert(o); }
 private:
  int insert(const Order& o) { if (o.ok && x) return 1; return 0; }
};
int PgRepo::table() { return 0; }
struct S { void m() {} };
namespace { int hidden() { return 0; } }
}}
template <class T> void free_fn(T t) { [&](int x) { return x; }(1); }
`,
		decls: "PgRepo/class PgRepo::PgRepo/ctor PgRepo::count/method PgRepo::save/method -PgRepo::insert/method " +
			"PgRepo::table/method S/struct S::m/method -hidden/function free_fn/function",
		imports: "file:repo.hpp", ns: "acme,acme.store",
		cx: map[string]string{"PgRepo::insert": "3/1", "free_fn": "1/1"},
	}.run(t)
}

func TestParsePHP(t *testing.T) {
	genCase{path: "src/OrderService.php", src: `<?php
namespace Acme\Orders;
use Acme\Store\PgRepo;
use Acme\Util\{Strings, Numbers as N};
use function Acme\Util\clean;
require_once __DIR__ . '/bootstrap.php';
/** Service. */
final class OrderService extends Base implements IService {
    use Loggable;
    private PgRepo $repo;
    public function __construct(PgRepo $repo) { $this->repo = $repo; }
    public function place(Order $o): int {
        if ($o && $o->ok || $x ?? null) { return 1; }
        foreach ($xs as $x) { $y = $x ? 1 : 2; }
        return match(1) { 1 => 2, default => 3 };
    }
    protected static function helper(): int { return 1; }
}
interface IService { public function place(Order $o): int; }
function top_level($a) { return new OrderService(new PgRepo()); }
`,
		decls: "OrderService/class OrderService::__construct/ctor OrderService::place/method -OrderService::helper/method " +
			"IService/interface IService::place/method top_level/function",
		imports: "Acme.Store.PgRepo, Acme.Util.Strings, Acme.Util.Numbers as N, Acme.Util.clean, file:./bootstrap.php",
		ns:      "Acme.Orders",
		cx:      map[string]string{"OrderService::place": "8/1"}, // if && || ?? foreach ?: match-arm
	}.run(t)
	f := ParseGen("x.php", "<?php\nclass A extends B implements C, \\D { use T; }\n")
	if got := strings.Join(f.Decls[0].Supers, ","); got != "B,C,D,T" {
		t.Errorf("supers = %s", got)
	}
}

func TestParseScala(t *testing.T) {
	genCase{path: "src/main/scala/acme/OrderService.scala", src: `package acme.orders
import acme.store.{PgRepo, Repo => R}
import acme.util._
class OrderService(repo: Repo) extends Base with Logging {
  private def validate(o: Order): Boolean = o.ok && true
  def place(o: Order): Int = {
    if (validate(o) || x) repo.save(o) else 0
    o match { case Order(1) => 1; case _ => 2 }
  }
}
object OrderService { def apply(r: Repo): OrderService = new OrderService(r) }
trait Repo { def save(o: Order): Int }
case class Order(id: Int)
def topLevel(x: Int) = x
`,
		decls: "OrderService/class -OrderService.validate/method OrderService.place/method OrderService/object " +
			"OrderService.apply/ctor Repo/trait Repo.save/method Order/class topLevel/function",
		imports: "acme.store.PgRepo, acme.store.Repo as R, acme.util.*", ns: "acme.orders",
		cx: map[string]string{"OrderService.place": "4/1", "OrderService.validate": "2/0"}, // if || case (not case _)
	}.run(t)
}

func TestParseKotlin(t *testing.T) {
	genCase{path: "src/main/kotlin/OrderService.kt", src: `package acme.orders
import acme.store.PgRepo
import acme.util.*
import acme.util.clean as cl
class OrderService(private val repo: Repo) : Base(), Logging {
    val count: Int get() = repo.count()
    constructor() : this(PgRepo())
    fun place(o: Order): Int {
        if (o.ok && x || y) { return 1 } else if (z) { return 2 }
        when (o.id) { 1 -> a(); else -> b() }
        return o?.id ?: 0
    }
    internal fun validate(o: Order) = 1
    companion object { fun make() = OrderService() }
}
interface Repo { fun save(o: Order): Int }
enum class State { NEW; fun x() {} }
object Registry { fun get() = 1 }
fun String.shout() = this.uppercase()
`,
		decls: "OrderService/class OrderService.count/property OrderService.constructor/ctor OrderService.place/method " +
			"-OrderService.validate/method OrderService.make/method Repo/interface Repo.save/method State/enum State.x/method " +
			"Registry/object Registry.get/method shout/function",
		imports: "acme.store.PgRepo, acme.util.*, acme.util.clean as cl", ns: "acme.orders",
		cx: map[string]string{"OrderService.place": "7/1"}, // if && || else-if when-arm ?:
	}.run(t)
	f := ParseGen("a.kt", "class A : B(), C, D<E> { }\n")
	if got := strings.Join(f.Decls[0].Supers, ","); got != "B,C,D" {
		t.Errorf("supers = %s", got)
	}
}

func TestParseRuby(t *testing.T) {
	src := `require_relative 'store/repo'
require 'json'
module Acme
  # Service.
  class OrderService < Base
    include Logging
    def initialize(repo)
      @repo = repo
    end
    def place(o)
      if o.ok && x || y
        @repo.save(o)
      elsif z then nil
      end
      items.each { |i| i.go } unless done?
    end
    def self.make = new(PgRepo.new)
    private
    def valid? = true
  end
end
def top_level; end
`
	genCase{path: "lib/orders.rb", src: src,
		decls: "Acme/module Acme::OrderService/class Acme::OrderService.initialize/ctor Acme::OrderService.place/method " +
			"Acme::OrderService.make/method -Acme::OrderService.valid?/method top_level/function",
		imports: "file:./store/repo.rb, file:json.rb",
		cx:      map[string]string{"Acme::OrderService.place": "6/1"}, // if && || elsif unless; the block nests
	}.run(t)
	f := ParseGen("lib/orders.rb", src)
	if got := strings.Join(f.Decls[1].Supers, ","); got != "Base,Logging" {
		t.Errorf("supers = %s", got)
	}
	if d := Innermost(GenDecls("lib/orders.rb", src), 12, 12); d == nil || d.Name != "Acme::OrderService.place" {
		t.Errorf("line 12 -> %v", d)
	}
}

func TestParseSwift(t *testing.T) {
	genCase{path: "Sources/Orders/OrderService.swift", src: `import Foundation
import Store
/// Service.
public final class OrderService: Base, Logging {
    private let repo: Repo
    public var count: Int { return repo.count() }
    public init(repo: Repo) { self.repo = repo }
    public func place(_ o: Order) -> Int {
        if o.ok && x || y { return 1 } else if z { return 2 }
        guard let z = o.z else { return 0 }
        switch o.id { case 1: break; default: break }
        return o.ok ? 1 : 0
    }
    func validate(_ o: Order) -> Int { 1 }
    subscript(i: Int) -> Int { i }
}
public protocol Repo { func save(_ o: Order) -> Int }
struct Order { let id: Int }
extension OrderService: Equatable { public static func same() -> Bool { true } }
actor Worker { func run() {} }
`,
		decls: "OrderService/class OrderService.count/property OrderService.init/ctor OrderService.place/method " +
			"-OrderService.validate/method -OrderService.subscript/method Repo/protocol -Repo.save/method -Order/struct " +
			"-OrderService/extension OrderService.same/method -Worker/actor -Worker.run/method",
		imports: "module:Foundation, module:Store",
		cx:      map[string]string{"OrderService.place": "8/1"}, // if && || else-if guard case ?:
	}.run(t)
	if m := langSwift.Module("pkg/Sources/Orders/A.swift"); m != "Orders" {
		t.Errorf("module = %q", m)
	}
}

func TestParseDart(t *testing.T) {
	genCase{path: "lib/src/order_service.dart", src: `library acme;
import 'package:acme/store/repo.dart';
import 'util.dart' as util show clean;
import 'dart:async';
part 'x.dart';
/// Service.
class OrderService extends Base with Logging implements IService {
  final Repo repo;
  OrderService(this.repo);
  OrderService.named() : repo = PgRepo();
  factory OrderService.make() => OrderService(PgRepo());
  int get count => repo.count();
  int place(Order o) {
    if (o.ok && x || y) { return 1; }
    for (final i in xs) { i ?? 0; }
    return o.ok ? 1 : 0;
  }
  int _validate(Order o) => o.ok ? 1 : 0;
  @override
  bool operator ==(Object other) => true;
}
abstract class IService { int place(Order o); }
mixin Logging { void log() {} }
extension Shout on String { String shout() => toUpperCase(); }
int topLevel() => 1;
`,
		decls: "OrderService/class OrderService.OrderService/ctor OrderService.named/ctor OrderService.make/ctor " +
			"OrderService.count/property OrderService.place/method -OrderService._validate/method OrderService.operator==/method " +
			"IService/class IService.place/method Logging/mixin Logging.log/method Shout/extension Shout.shout/method topLevel/function",
		imports: "file:lib/store/repo.dart, file:./util.dart as util, file:./x.dart",
		cx:      map[string]string{"OrderService.place": "7/1", "OrderService._validate": "2/0"}, // if && || for ?? ?:
	}.run(t)
	if f := ParseGen("lib/a.g.dart", "int x() => 1;\n"); !f.Generated {
		t.Errorf(".g.dart is generated")
	}
}

func TestLangTests(t *testing.T) {
	for p, want := range map[string]bool{
		"spec/models/order_spec.rb": true, "lib/order.rb": false, "app/src/test/kotlin/A.kt": true, "src/main/kotlin/ATest.kt": true,
		"Tests/OrdersTests/A.swift": true, "lib/a_test.dart": true, "tests/Unit/ATest.php": true, "src/A.php": false,
		"src/order_test.cc": true, "src/order.cc": false, "Orders.Tests.ps1": true, "scripts/test_deploy.sh": true,
		"src/test/scala/A.scala": true, "src/main/scala/ASpec.scala": true,
	} {
		if got := LangFor(p).IsTest(p); got != want {
			t.Errorf("IsTest(%s) = %v", p, got)
		}
	}
	if LangFor("x.go") != nil || LangFor("a.H") != langCpp {
		t.Errorf("LangFor")
	}
}

// The indexer's parse cache stores GenFiles with gob.
func TestGenFileGob(t *testing.T) {
	f := ParseGen("lib/a.rb", "require_relative 'b'\nclass A < B\n  def x = 1\nend\n")
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(f); err != nil {
		t.Fatal(err)
	}
	var g GenFile
	if err := gob.NewDecoder(&buf).Decode(&g); err != nil {
		t.Fatal(err)
	}
	if g.Lang() != langRuby || len(g.Decls) != 2 || g.Decls[1].Name != "A.x" || len(g.Toks) != len(f.Toks) || len(g.Imports) != 1 {
		t.Errorf("decoded %+v", g)
	}
}

func TestBlankData(t *testing.T) {
	rows := strings.Repeat("    0x58, 0x70, 0x63, /* U+0025 */ 0x8,\n#if DEPTH == 16\n    {.adv_w = 34, .box_w = 0},\n#endif\n", 60)
	src := "static const uint8_t a[] = {\n" + rows + "};\nint f(void) { return 1; }\nstatic const cmd_t t[] = {\n" +
		strings.Repeat("    {\"x\", handler},\n", 200) + "};\n"
	got := blankData(src)
	if strings.Count(got, "\n") != strings.Count(src, "\n") || strings.Contains(got, "0x58") || !strings.Contains(got, "handler") {
		t.Errorf("blankData kept data or dropped code")
	}
	if d := Innermost(GenDecls("a.c", src), 243, 243); d == nil || d.Name != "f" {
		t.Errorf("f -> %v", d)
	}
}
