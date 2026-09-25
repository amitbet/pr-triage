package decls

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseCS(t *testing.T) {
	src := `// <copyright>
using System;
global using Acme.Common;
using static Acme.Util.Strings;
using Repo = Acme.Store.PgRepo;

#nullable enable
namespace Acme.Orders
{
    /// <summary>Orders.</summary>
    [Service]
    public partial class OrderService : Base<Order>, IHandler, IDisposable where T : class, new()
    {
        private readonly IRepo _repo; // trailing comment
        private const string Url = "/orders/api/{id}";
        private static readonly string Raw = @"C:\x ""quoted"" {";
        private readonly Func<int> _f = () => { return 1; };
        public event EventHandler? Changed;

        public OrderService(IRepo repo) : base(repo) { _repo = repo; }

        public string Name { get; set; } = "x";
        public int Count => _repo.Count();

        // Places an order.
        [HttpPost]
        public async Task<(int Id, string Msg)> PlaceAsync<TOrder>(TOrder o) where TOrder : Order
        {
            var s = $"{o.Id}: {(o.Ok ? "ok" : "no")} }}";
            if (o == null) { return default; }
            return (await _repo.SaveAsync(o), s);
        }

        int Place(int n) => n;

        public int this[int i] { get { return i; } }

        public static OrderService operator +(OrderService a, OrderService b) => a;
        public static implicit operator string(OrderService s) => s.Name;

        ~OrderService() { }

        public enum State { New = 1, Done = 2 }

        public interface IListener { void OnEvent(string e); }

        public record Pair(string A, int B) : IComparable<Pair>
        {
            public int CompareTo(Pair? o) => 0;
        }

        public delegate void Handler<T>(T value);
    }

    public static class Ext
    {
        public static string Shout(this string s) => s.ToUpper();
    }
}
`
	f := ParseCS("src/Acme.Orders/OrderService.cs", src)
	if got := strings.Join(f.Namespaces, ","); got != "Acme.Orders" {
		t.Errorf("namespaces = %s", got)
	}
	var us []string
	for _, u := range f.Usings {
		us = append(us, fmt.Sprintf("%s:%s:%v:%v", u.Path, u.Alias, u.Static, u.Global))
	}
	if got := strings.Join(us, " "); got != "System::false:false Acme.Common::false:true Acme.Util.Strings::true:false Acme.Store.PgRepo:Repo:false:false" {
		t.Errorf("usings = %s", got)
	}
	var names []string
	byName := map[string]*CSDecl{}
	for _, d := range f.Decls {
		names = append(names, d.Name+"/"+d.Kind)
		if byName[d.Name] == nil {
			byName[d.Name] = d
		}
	}
	want := "OrderService/class OrderService.OrderService/ctor OrderService.Name/property OrderService.Count/property " +
		"OrderService.PlaceAsync/method OrderService.Place/method OrderService.this[]/property " +
		"OrderService.operator+/method OrderService.operator string/method OrderService.~OrderService/method " +
		"OrderService.State/enum OrderService.IListener/interface OrderService.IListener.OnEvent/method " +
		"OrderService.Pair/record OrderService.Pair.CompareTo/method OrderService.Handler/delegate " +
		"Ext/class Ext.Shout/method"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("decls:\n got %s\nwant %s", got, want)
	}
	cls := byName["OrderService"]
	if cls.Line != 10 || !cls.Exported || !cls.Partial || cls.Namespace != "Acme.Orders" || strings.Join(cls.Supers, ",") != "Base,IHandler,IDisposable" {
		t.Errorf("class = %+v", cls)
	}
	if p := byName["OrderService.PlaceAsync"]; p.Line != 25 || p.EndLine != 32 || !p.Exported {
		t.Errorf("PlaceAsync = line %d-%d exported %v", p.Line, p.EndLine, p.Exported)
	}
	if !byName["OrderService.IListener.OnEvent"].Exported {
		t.Errorf("interface members are public")
	}
	if byName["OrderService.Place"].Exported {
		t.Errorf("Place is private")
	}
	if !byName["Ext.Shout"].Ext || byName["OrderService.Place"].Ext {
		t.Errorf("extension flags")
	}
	// A line in a method belongs to the method, a field line to the class.
	ds := CSDecls(src)
	if d := Innermost(ds, 29, 29); d == nil || d.Name != "OrderService.PlaceAsync" {
		t.Errorf("line 29 -> %v", d)
	}
	if d := Innermost(ds, 14, 14); d == nil || d.Name != "OrderService" {
		t.Errorf("line 14 -> %v", d)
	}
}

func TestParseCSFileScopedNamespace(t *testing.T) {
	src := "namespace A.B;\n\nusing C;\n\npublic record R(int X);\n\ninternal struct S { public void M() { } }\n"
	f := ParseCS("", src)
	var names []string
	for _, d := range f.Decls {
		names = append(names, d.Namespace+":"+d.Name+"/"+d.Kind)
	}
	if got := strings.Join(names, " "); got != "A.B:R/record A.B:S/struct A.B:S.M/method" {
		t.Errorf("decls = %s", got)
	}
	if len(f.Usings) != 1 || f.Usings[0].Path != "C" {
		t.Errorf("usings = %+v", f.Usings)
	}
}
