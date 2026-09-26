// Header triage form: starts a job and polls it until the result is ready.
import { $, esc, api, postJSON } from "./util.js";
import { jobSettings } from "./settings.js";

let onDone = async () => {};

async function triage() {
  const url = $("#url").value.trim();
  if (!url) return;
  const isPath = !/^https?:\/\//.test(url) && !/^[^\s]+\/[^\s]+#\d+$/.test(url);
  const body = { [isPath ? "path" : "url"]: url, force: $("#force").checked, ...jobSettings() };
  $("#go").disabled = true;
  $("#main").innerHTML = `<div class="progress">starting…</div>`;
  try {
    const job = await postJSON("/api/triage", body);
    for (;;) {
      const j = await api(`/api/jobs/${job.id}`);
      if (j.status === "done") { await onDone(j.key); break; }
      if (j.status === "error") throw new Error(j.error);
      const pct = j.total ? Math.round((100 * j.done) / j.total) : 0;
      const repo = isPath ? "this repo" : j.url.split("/")[4] || "this repo";
      const what = {
        fetch: "fetching PR and diffing",
        inspect: "reading the local changes",
        clone: `${repo} is not in the code map: cloning it into the workspace…`,
        codemap: `${repo} is not in the code map: building it before triage (a few minutes the first time)…`,
      }[j.stage] || `${j.stage} ${j.done}/${j.total} units`;
      $("#main").innerHTML = `<div class="progress">${esc(j.url)}<br>${esc(what)}<div class="bar"><div style="width:${pct}%"></div></div></div>`;
      await new Promise((res) => setTimeout(res, 700));
    }
  } catch (e) {
    $("#main").innerHTML = `<div class="error">${esc(e.message)}</div>`;
  } finally {
    $("#go").disabled = false;
  }
}

// initTriage wires the form; done(key) shows the finished result.
export function initTriage(done) {
  onDone = done;
  $("#go").onclick = triage;
  $("#url").addEventListener("keydown", (e) => e.key === "Enter" && triage());
}

// triageURL fills the form with a PR link and starts triaging it.
export function triageURL(url) {
  $("#url").value = url;
  triage();
}
