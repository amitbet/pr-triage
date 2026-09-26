// Local fix jobs. A completed job opens its reviewed result and worktree path.
import { $, esc, postJSON } from "./util.js";
import { S } from "./state.js";
import { fixSettings } from "./settings.js";
import { watchJob, refreshJobs } from "./jobs.js";

let busy = false;

async function startFix(all, unitID = "", issue = 0) {
  if (busy || !S.result) return false;
  busy = true;
  const body = { key: S.result.key, all, unit_id: unitID, issue, ...fixSettings() };
  if (S.result.pr.local_path) body.location = "worktree";
  $("#main").innerHTML = `<div class="progress">starting local fix…</div>`;
  try {
    const job = await postJSON("/api/fix", body);
    refreshJobs();
    watchJob(job.id);
  } catch (e) {
    $("#main").innerHTML = `<div class="error">${esc(e.message)}</div>`;
  } finally {
    busy = false;
  }
  return false;
}

export const actions = {
  "fix-issue": (el) => startFix(false, el.dataset.unit, Number(el.dataset.issue)),
  "fix-all": () => startFix(true),
};
