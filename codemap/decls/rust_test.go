package decls

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestParseRust(t *testing.T) {
	src := `//! Orders.
use crate::store::{self, Repo, pg::PgRepo as Pg};
pub use super::util::*;
mod orders;

/// An order.
#[derive(Debug)]
pub struct Order<T: Clone> {
    pub id: u32,
    items: Vec<T>,
    repo: Box<dyn Repo>,
}

pub(crate) struct Pair(pub u8, Pg);

pub enum State { New, Done(u8) }

pub trait Handler: Send + Sync {
    fn handle(&self, o: &Order<u8>) -> bool;
    fn name(&self) -> String { String::new() }
}

impl<T: Clone> Handler for Order<T> {
    fn handle(&self, o: &Order<u8>) -> bool {
        if self.id > 0 && o.id < 3 {
            return true;
        }
        false
    }
}

impl Order<u8> {
    const MAX: u32 = 3;

    // Makes one.
    pub fn new(id: u32) -> Result<Self, String> {
        let s = PgRepo::new(1);
        let mut x: Pg = Pg::default();
        let o = Order { id, items: vec![], repo: Box::new(s) };
        let Some(v) = maybe() else { return Err(e) };
        match id {
            0 => {}
            n if n > MAX => {}
            _ => {}
        }
        for i in 0..3 { let c = |y| y + i; }
        Ok(o)
    }
}

pub fn run<R>(r: &R, v: impl Handler) where R: Repo {}

mod inner {
    pub fn f() {}
    macro_rules! m { () => {} }
}

#[cfg(test)]
mod tests;

#[cfg(test)]
#[path = "orders_tests.rs"]
mod more;

#[cfg(test)]
mod unit {
    use super::*;
    #[tokio::test(flavor = "multi_thread")]
    fn works() { run(); }
}

#[cfg(not(test))]
fn prod() {}
`
	f := ParseRust("src/orders.rs", src)
	var names []string
	by := map[string]*RsDecl{}
	for _, d := range f.Decls {
		names = append(names, fmt.Sprintf("%s/%s:%d-%d", d.Name, d.Kind, d.Line, d.EndLine))
		if by[d.Name] == nil { // the struct, not its impls
			by[d.Name] = d
		}
	}
	want := "Order/struct:6-12 Pair/struct:14-14 State/enum:16-16 Handler/trait:18-21 Handler::handle/method:19-19 " +
		"Handler::name/method:20-20 Order/impl:23-30 Order::handle/method:24-29 Order/impl:32-49 Order::MAX/const:33-33 " +
		"Order::new/method:35-48 run/fn:51-51 inner/mod:53-56 inner::f/fn:54-54 inner::m/macro:55-55 unit/mod:65-70 " +
		"unit::works/fn:68-69 prod/fn:72-73"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("decls:\n got %s\nwant %s", got, want)
	}
	var uses []string
	for _, u := range f.Uses {
		uses = append(uses, fmt.Sprintf("%s|%s|%v|%s|%v", u.Path, u.Local, u.Wildcard, u.Mod, u.Pub))
	}
	if got, want := strings.Join(uses, " "), "crate::store|store|false||false crate::store::Repo|Repo|false||false "+
		"crate::store::pg::PgRepo|Pg|false||false super::util||true||true super||true|unit|false"; got != want {
		t.Errorf("uses:\n got %s\nwant %s", got, want)
	}
	var mods []string
	for _, m := range f.Mods {
		mods = append(mods, fmt.Sprintf("%s|%s|%v", m.Mod, m.Path, m.Test))
	}
	if got := strings.Join(mods, " "); got != "orders||false tests||true more|orders_tests.rs|true" {
		t.Errorf("mods = %s", got)
	}
	o := by["Order"]
	if !o.Exported || o.Fields["repo"] != "Repo" || o.Fields["items"] != "Vec" || o.Bounds["T"] != "Clone" {
		t.Errorf("Order = %+v", o)
	}
	if p := by["Pair"]; p.Exported || p.Fields["0"] != "" || p.Fields["1"] != "Pg" {
		t.Errorf("Pair = %+v", p)
	}
	if s := strings.Join(by["State"].Variants, ","); s != "New,Done" {
		t.Errorf("variants = %s", s)
	}
	if s := strings.Join(by["Handler"].Supers, ","); s != "Send,Sync" {
		t.Errorf("supers = %s", s)
	}
	if h := by["Order::handle"]; h.Owner != "Order" || h.Trait != "Handler" || !h.Exported || h.Cyclo != 3 || h.Nest != 1 {
		t.Errorf("handle = %+v", h)
	}
	if !by["Handler::name"].Exported || by["Handler::name"].Owner != "Handler" {
		t.Errorf("trait methods of a pub trait are exported")
	}
	n := by["Order::new"]
	if n.Returns != "Self" || !n.Exported || n.Owner != "Order" || n.Trait != "" {
		t.Errorf("new = %+v", n)
	}
	if n.Vars["s"] != "=PgRepo::new" || n.Vars["x"] != "Pg" || n.Vars["o"] != "Order" || n.Vars["id"] != "" {
		t.Errorf("vars = %v", n.Vars)
	}
	var locals []string
	for l := range n.Locals {
		locals = append(locals, l)
	}
	sort.Strings(locals)
	if got := strings.Join(locals, ","); got != "c,i,id,n,o,s,v,x,y" {
		t.Errorf("locals = %s", got)
	}
	if n.Cyclo != 5 || n.Nest != 2 { // match arms 0 and n, n's guard, for; a closure in the loop
		t.Errorf("new cx = %d/%d", n.Cyclo, n.Nest)
	}
	if r := by["run"]; r.Bounds["R"] != "Repo" || r.Vars["r"] != "R" || r.Vars["v"] != "Handler" {
		t.Errorf("run = %+v", r)
	}
	if by["inner::m"].Exported || by["inner::f"].Mod != "inner" {
		t.Errorf("inner items")
	}
	if !by["unit"].Test || !by["unit::works"].Test || by["run"].Test || by["prod"].Test {
		t.Errorf("test flags")
	}
	ds := RustDecls(src)
	if d := Innermost(ds, 26, 26); d == nil || d.Name != "Order::handle" {
		t.Errorf("line 26 -> %v", d)
	}
	if d := Innermost(ds, 23, 23); d == nil || d.Name != "Order" {
		t.Errorf("line 23 -> %v", d)
	}
}
