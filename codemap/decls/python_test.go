package decls

import (
	"fmt"
	"strings"
	"testing"
)

func TestParsePy(t *testing.T) {
	src := `"""Module doc."""
import os, json as j
import app.db.models
from . import util
from ..core.base import Base, Mixin as M
from app.api import (
    client,
    routes,
)
from helpers import *

TIMEOUT: int = 30
URL = f"/orders/{TIMEOUT}/x"
TIMEOUT = 40


# Runs the job.
@decorator(arg=1)
async def run(job, *, retries=3):
    if job and retries:
        for x in job:
            if x:
                pass
    return client.send(job)


class Service(Base, M, metaclass=Meta):
    """Doc."""

    def __init__(self, repo: Repo):
        self.repo = repo

    @property
    def name(self):
        def inner():
            return 1
        return inner()

    class Config:
        def load(self):
            return \
                1

def tail(): return [i for i in range(3)
                    if i]
`
	f := ParsePy("app/svc/service.py", src)
	var ims []string
	for _, im := range f.Imports {
		ims = append(ims, fmt.Sprintf("%s|%s|%s|%v", im.Module, im.Name, im.Local, im.As))
	}
	want := "os||os|false json||j|true app.db.models||app|false .|util|util|false ..core.base|Base|Base|false " +
		"..core.base|Mixin|M|false app.api|client|client|false app.api|routes|routes|false helpers|*||false"
	if got := strings.Join(ims, " "); got != want {
		t.Errorf("imports:\n got %s\nwant %s", got, want)
	}
	var names []string
	byName := map[string]*PyDecl{}
	for _, d := range f.Decls {
		names = append(names, fmt.Sprintf("%s/%s:%d-%d", d.Name, d.Kind, d.Line, d.EndLine))
		byName[d.Name] = d
	}
	want = "TIMEOUT/var:12-12 URL/var:13-13 run/function:17-24 Service/class:27-42 Service.__init__/method:30-31 " +
		"Service.name/method:33-37 Service.Config/class:39-42 Service.Config.load/method:40-42 tail/function:44-45"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("decls:\n got %s\nwant %s", got, want)
	}
	if b := strings.Join(byName["Service"].Bases, ","); b != "Base,M" {
		t.Errorf("bases = %s", b)
	}
	if byName["Service.__init__"].Exported || !byName["run"].Exported {
		t.Errorf("exported: __init__=%v run=%v", byName["Service.__init__"].Exported, byName["run"].Exported)
	}
	// f-string expressions are lexed: TIMEOUT is used on line 13.
	used := false
	for _, tk := range f.Toks {
		if tk.Line == 13 && tk.Kind == 'i' && tk.Text == "TIMEOUT" {
			used = true
		}
	}
	if !used {
		t.Errorf("f-string expression not lexed")
	}
	ds := PyDecls(src)
	if d := Innermost(ds, 42, 42); d == nil || d.Name != "Service.Config.load" {
		t.Errorf("line 42 -> %v", d)
	}
}
