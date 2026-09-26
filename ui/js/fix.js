// Local fix jobs. A completed job opens its reviewed result and worktree path.
import { $, esc, api, postJSON } from "./util.js";
import { S } from "./state.js";
import { fixSettings } from "./settings.js";

let onDone = async () => {};
let busy = false;

export function initFix(done) { onDone = done; }

async function startFix(all, unitID = "", issue = 0) {
  if (busy || !S.result) return false;
  busy = true;
  const body = { key: S.result.key, all, unit_id: unitID, issue, ...fixSettings() };
  if (S.result.pr.local_path) body.location = "worktree";
  $("#main").innerHTML = `<div class="progress">starting local fix…</div>`;
  try {
    const job = await postJSON("/api/fix", body);
    for (;;) {
      const j = await api(`/api/jobs/${job.id}`);
      if (j.status === "done") { await onDone(j.key); break; }
      if (j.status === "error") throw new Error(j.error);
      $("#main").innerHTML = `<div class="progress">${esc(j.stage || "fixing")} ${j.done || 0}/${j.total || 1}<div class="bar"><div style="width:${j.total ? (100 * j.done / j.total) : 0}%"></div></div></div>`;
      await new Promise((resolve) => setTimeout(resolve, 700));
    }
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
