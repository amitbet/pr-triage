package cx

import "testing"

func TestGoDecls(t *testing.T) {
	src := `package p

// Flat has no branches.
func Flat() int { return 1 }

func (s *S) Branchy(xs []int, ok bool) int {
	n := 0
	for _, x := range xs { // +1, depth 1
		if x > 0 && ok { // +2, depth 2
			n++
		} else if x < 0 { // +1, same depth
			n--
		} else {
			switch x { // depth 3
			case 1, 2: // +1
			default:
			}
		}
	}
	return n
}

var handler = func(v int) bool { return v > 0 || v < -10 } // +1, func lit depth 1
`
	got := map[string]Stat{}
	for _, d := range GoDecls([]byte(src)) {
		got[d.Name] = d.Stat
	}
	want := map[string]Stat{
		"Flat":         {Cyclo: 1, Nest: 0},
		"(*S).Branchy": {Cyclo: 6, Nest: 3},
		"var handler":  {Cyclo: 2, Nest: 1},
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %+v, want %+v", name, got[name], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("decls = %v", got)
	}
}

func TestTSDecls(t *testing.T) {
	src := `export function pick(a?: number, b = 2) {
  if (a && b) {
    for (const x of xs) {
      const o = { k: 1 };
      return x ?? o.k;
    }
  }
  return a ? a : b?.toString();
}

export const Button = ({ on }) => {
  return on || null;
};
`
	got := map[string]Stat{}
	for _, d := range TSDecls("x.ts", src) {
		got[d.Name] = d.Stat
	}
	// pick: if, &&, for, ??, ternary = 1+5; nesting if -> for = 2 (object literal braces don't count).
	if s := got["pick"]; s.Cyclo != 6 || s.Nest != 2 {
		t.Errorf("pick = %+v", s)
	}
	if s := got["Button"]; s.Cyclo != 2 || s.Nest != 0 {
		t.Errorf("Button = %+v", s)
	}
}

func TestJavaDecls(t *testing.T) {
	src := `class A {
    int flat() { return 1; }

    int branchy(List<? extends X> xs, boolean ok) {
        int n = 0;
        for (X x : xs) {                 // +1, depth 1
            if (x.v() > 0 && ok) {       // +2, depth 2
                n++;
            } else {
                xs.forEach(y -> {        // depth 3
                    n += y.v() > 1 ? 1 : 0; // +1
                });
            }
        }
        try { n++; } catch (E e) { n--; } // +1
        return n;
    }
}
`
	got := map[string]Stat{}
	for _, d := range JavaDecls(src) {
		got[d.Name] = d.Stat
	}
	if s := got["A.flat"]; s.Cyclo != 1 || s.Nest != 0 {
		t.Errorf("flat = %+v", s)
	}
	if s := got["A.branchy"]; s.Cyclo != 6 || s.Nest != 3 {
		t.Errorf("branchy = %+v", s)
	}
	if _, ok := got["A"]; ok {
		t.Errorf("types are not measured")
	}
}

func TestPyDecls(t *testing.T) {
	src := `def flat():
    return 1


class C:
    def branchy(self, xs, ok):
        n = 0
        for x in xs:                # +1, depth 1
            if x > 0 and ok:        # +2, depth 2
                n += 1
            elif x < 0:             # +1
                n -= 1
        try:
            n = [y for y in xs if y]  # +2
        except ValueError:          # +1
            pass
        match n:
            case 1:                 # +1
                pass
        return n
`
	got := map[string]Stat{}
	for _, d := range PyDecls(src) {
		got[d.Name] = d.Stat
	}
	if s := got["flat"]; s.Cyclo != 1 || s.Nest != 0 {
		t.Errorf("flat = %+v", s)
	}
	if s := got["C.branchy"]; s.Cyclo != 9 || s.Nest != 2 {
		t.Errorf("branchy = %+v", s)
	}
	if _, ok := got["C"]; ok {
		t.Errorf("classes are not measured")
	}
}

func TestCSDecls(t *testing.T) {
	src := `class A {
    int Flat() => 1;

    string? Name { get { return _n ?? "x"; } }

    int Branchy(List<int?> xs, bool ok) {
        int? n = 0;
        foreach (var x in xs) {             // +1, depth 1
            if (x > 0 && ok) {              // +2, depth 2
                n++;
            } else {
                xs.ForEach(y => {           // depth 3
                    n += y > 1 ? 1 : 0;     // +1
                });
            }
        }
        var p = new Point(1, 2) { Z = 3 };  // initializer, not a block
        try { n = p?.Z; } catch (E e) { n--; } // +1
        return n ?? 0;                      // +1
    }
}
`
	got := map[string]Stat{}
	for _, d := range CSDecls(src) {
		got[d.Name] = d.Stat
	}
	if s := got["A.Flat"]; s.Cyclo != 1 || s.Nest != 0 {
		t.Errorf("Flat = %+v", s)
	}
	if s := got["A.Name"]; s.Cyclo != 2 || s.Nest != 0 {
		t.Errorf("Name = %+v", s)
	}
	if s := got["A.Branchy"]; s.Cyclo != 7 || s.Nest != 3 {
		t.Errorf("Branchy = %+v", s)
	}
	if _, ok := got["A"]; ok {
		t.Errorf("types are not measured")
	}
}

func TestRustDecls(t *testing.T) {
	src := `impl S {
    fn flat(&self) -> u32 { 1 }

    fn branchy(&self, xs: &[i32], ok: bool) -> i32 {
        let mut n = 0;
        for x in xs {                        // +1, depth 1
            if *x > 0 && ok {                // +2, depth 2
                n += 1;
            } else if *x < 0 {               // +1, same depth
                xs.iter().for_each(|y| {     // depth 3
                    n += match y { 0 => 1, _ => 0 }; // +1 (not _), depth 4
                });
            }
        }
        n
    }
}
`
	got := map[string]Stat{}
	for _, d := range RustDecls(src) {
		got[d.Name] = d.Stat
	}
	if s := got["S::flat"]; s.Cyclo != 1 || s.Nest != 0 {
		t.Errorf("flat = %+v", s)
	}
	if s := got["S::branchy"]; s.Cyclo != 6 || s.Nest != 4 {
		t.Errorf("branchy = %+v", s)
	}
	if _, ok := got["S"]; ok {
		t.Errorf("impls are not measured")
	}
}
