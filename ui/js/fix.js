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

// fixDisabled disables a fix button when a local checkout has uncommitted
// changes and no separate worktree to fix in.
export const fixDisabled = () => S.result.pr.local_path && S.result.pr.uncommitted && !S.result.local_fix_dir
  ? 'disabled title="Commit changes before fixing in a separate worktree"' : "";

// issueFixButton starts a local fix job for issue i of unit u.
export function issueFixButton(u, i, cls = "details-btn") {
  return `<button class="${cls}" data-act="fix-issue" data-unit="${esc(u.id)}" data-issue="${i}" ${fixDisabled()}>Fix issue</button>`;
}

export const actions = {
  "fix-issue": (el) => startFix(false, el.dataset.unit, Number(el.dataset.issue)),
  "fix-all": () => startFix(true),
};
