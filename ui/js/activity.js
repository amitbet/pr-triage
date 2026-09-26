// Activity panel: the running job's threads (gh and git commands, each
// unit's classify and review calls) with their log lines, each collapsible.
// Updated in place so open threads and scroll positions survive a refresh.
import { esc, api } from "./util.js";

const KIND = { gh: "gh", git: "git", llm: "model", job: "job", "pr-manager": "codemap" };
const MAX_DOM_LINES = 400;

function took(t) {
  const end = t.end ? new Date(t.end) : new Date();
  const s = Math.max(0, (end - new Date(t.start)) / 1000);
  return s < 60 ? `${s.toFixed(1)}s` : `${Math.floor(s / 60)}m${String(Math.round(s % 60)).padStart(2, "0")}s`;
}

const clock = (t) => new Date(t).toLocaleTimeString([], { hour12: false });

// mountActivity renders the panel into el and returns refresh(), which
// fetches the job's log and updates the panel.
export function mountActivity(el, jobId) {
  el.innerHTML = `<div class="act">
    <div class="act-head">
      <b>Activity</b><span class="act-count"></span><span class="spacer"></span>
      <label class="act-opt"><input type="checkbox" class="act-follow" checked> follow running</label>
      <label class="act-opt"><input type="checkbox" class="act-hide"> hide finished</label>
      <button type="button" class="linkbtn act-open">expand all</button>
      <button type="button" class="linkbtn act-close">collapse all</button>
    </div>
    <div class="act-list"></div>
  </div>`;
  const list = el.querySelector(".act-list");
  const follow = el.querySelector(".act-follow");
  const hide = el.querySelector(".act-hide");
  const rows = new Map(); // thread id -> { el, seen, touched }
  hide.onchange = () => list.classList.toggle("hide-done", hide.checked);
  el.querySelector(".act-open").onclick = () => rows.forEach((r) => { r.el.open = true; r.touched = true; });
  el.querySelector(".act-close").onclick = () => rows.forEach((r) => { r.el.open = false; r.touched = true; });

  function row(t) {
    let r = rows.get(t.id);
    if (r) return r;
    const d = document.createElement("details");
    d.className = "act-thread";
    d.innerHTML = `<summary><span class="act-dot"></span><span class="act-kind"></span><span class="act-name"></span><span class="act-time"></span></summary><pre class="act-lines"></pre>`;
    d.querySelector(".act-kind").textContent = KIND[t.kind] || t.kind;
    d.querySelector(".act-name").textContent = t.name;
    d.querySelector(".act-name").title = t.name;
    r = { el: d, seen: 0, touched: false };
    // A thread the user opened or closed keeps that state.
    d.querySelector("summary").addEventListener("click", () => { r.touched = true; });
    d.open = t.kind === "job";
    rows.set(t.id, r);
    list.appendChild(d);
    return r;
  }

  function update(t) {
    const r = row(t);
    const d = r.el;
    d.dataset.status = t.status;
    d.querySelector(".act-time").textContent = `${t.lines.length ? t.lines.length + (t.dropped || 0) + " lines · " : ""}${took(t)}`;
    if (!r.touched && follow.checked && t.kind !== "job") d.open = t.status === "running";
    const total = (t.dropped || 0) + t.lines.length;
    const fresh = t.lines.slice(Math.max(0, t.lines.length - (total - r.seen)));
    if (!fresh.length) return;
    r.seen = total;
    const pre = d.querySelector(".act-lines");
    const atBottom = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 20;
    const frag = document.createDocumentFragment();
    for (const l of fresh) {
      const div = document.createElement("div");
      div.className = /^(error|✗|stderr:)/.test(l.text) ? "act-line err" : "act-line";
      div.innerHTML = `<span class="act-ts">${esc(clock(l.t))}</span>${esc(l.text)}`;
      frag.appendChild(div);
    }
    pre.appendChild(frag);
    while (pre.childElementCount > MAX_DOM_LINES) pre.firstElementChild.remove();
    if (atBottom) pre.scrollTop = pre.scrollHeight;
  }

  return async function refresh() {
    const threads = (await api(`/api/jobs/${jobId}/log`)).map((t) => ({ ...t, lines: t.lines || [] }));
    const listAtBottom = list.scrollHeight - list.scrollTop - list.clientHeight < 20;
    threads.forEach(update);
    const running = threads.filter((t) => t.status === "running").length;
    const failed = threads.filter((t) => t.status === "error").length;
    el.querySelector(".act-count").textContent =
      ` ${threads.length} threads · ${running} running${failed ? ` · ${failed} failed` : ""}`;
    if (listAtBottom) list.scrollTop = list.scrollHeight;
  };
}
