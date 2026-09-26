// Header triage form: starts a job and shows it in the job view.
import { $, esc, postJSON } from "./util.js";
import { jobSettings } from "./settings.js";
import { watchJob, refreshJobs } from "./jobs.js";

async function triage() {
  const url = $("#url").value.trim();
  if (!url) return;
  const isPath = !/^https?:\/\//.test(url) && !/^[^\s]+\/[^\s]+#\d+$/.test(url);
  const body = { [isPath ? "path" : "url"]: url, force: $("#force").checked, ...jobSettings() };
  $("#go").disabled = true;
  $("#main").innerHTML = `<div class="progress">starting…</div>`;
  try {
    const job = await postJSON("/api/triage", body);
    refreshJobs();
    watchJob(job.id);
  } catch (e) {
    $("#main").innerHTML = `<div class="error">${esc(e.message)}</div>`;
  } finally {
    $("#go").disabled = false;
  }
}

// initTriage wires the form.
export function initTriage() {
  $("#go").onclick = triage;
  $("#url").addEventListener("keydown", (e) => e.key === "Enter" && triage());
}

// triageURL fills the form with a PR link and starts triaging it.
export function triageURL(url) {
  $("#url").value = url;
  triage();
}
