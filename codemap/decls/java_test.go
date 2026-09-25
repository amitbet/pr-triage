package decls

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseJava(t *testing.T) {
	src := `package com.acme.orders;

import java.util.List;
import com.acme.store.*;
import static com.acme.util.Strings.trim;

/**
 * Orders.
 */
@Service
public class OrderService extends Base implements Handler<Order>, Closeable {
    private final OrderRepo repo; // trailing comment
    private static final String URL = "/orders/api";
    private final Runnable r = () -> { run(); };

    public OrderService(OrderRepo repo) { this.repo = repo; }

    // Places an order.
    @Override
    public <T> List<T> place(Order o) throws IOException {
        if (o == null) { return null; }
        return repo.save(o);
    }

    int place(int n) { return n; }

    static { init(); }

    enum State { NEW("n"), DONE("d") { void x() {} }; final String s; State(String s) { this.s = s; } String code() { return s; } }

    interface Listener { void onEvent(String e); }

    record Pair(String a, int b) implements Comparable<Pair> {
        public int compareTo(Pair o) { return 0; }
    }
}

@interface Audit { String value() default ""; }
`
	f := ParseJava("src/main/java/com/acme/orders/OrderService.java", src)
	if f.Package != "com.acme.orders" {
		t.Errorf("package = %q", f.Package)
	}
	var ims []string
	for _, im := range f.Imports {
		ims = append(ims, fmt.Sprintf("%s:%v:%v", im.Path, im.Static, im.Wildcard))
	}
	if got := strings.Join(ims, " "); got != "java.util.List:false:false com.acme.store:false:true com.acme.util.Strings.trim:true:false" {
		t.Errorf("imports = %s", got)
	}
	var names []string
	byName := map[string]*JavaDecl{}
	for _, d := range f.Decls {
		names = append(names, d.Name+"/"+d.Kind)
		if byName[d.Name] == nil {
			byName[d.Name] = d
		}
	}
	want := "OrderService/class OrderService.OrderService/ctor OrderService.place/method OrderService.place/method " +
		"OrderService.State/enum OrderService.State.State/ctor OrderService.State.code/method " +
		"OrderService.Listener/interface OrderService.Listener.onEvent/method " +
		"OrderService.Pair/record OrderService.Pair.compareTo/method Audit/annotation Audit.value/method"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("decls:\n got %s\nwant %s", got, want)
	}
	cls := byName["OrderService"]
	if cls.Line != 7 || !cls.Exported || strings.Join(cls.Supers, ",") != "Base,Handler,Closeable" {
		t.Errorf("class = %+v", cls)
	}
	if p := byName["OrderService.place"]; p.Line != 18 || p.EndLine != 23 || !p.Exported {
		t.Errorf("place = line %d-%d exported %v", p.Line, p.EndLine, p.Exported)
	}
	if !byName["OrderService.Listener.onEvent"].Exported {
		t.Errorf("interface methods are public")
	}
	// A line in a method belongs to the method, a field line to the class.
	ds := JavaDecls(src)
	if d := Innermost(ds, 21, 21); d == nil || d.Name != "OrderService.place" {
		t.Errorf("line 21 -> %v", d)
	}
	if d := Innermost(ds, 13, 13); d == nil || d.Name != "OrderService" {
		t.Errorf("line 13 -> %v", d)
	}
}
