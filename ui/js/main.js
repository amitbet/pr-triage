// Entry point: renders the page for the current PR and routes data-act
// clicks to the component that owns them.
//
// Components (review.js, walkthrough.js, treemap.js, comments.js, diff.js)
// export `actions`: handlers keyed by data-act. A handler changes state and
// returns nothing to have the page re-rendered, or false when it rendered
// (or deliberately didn't) itself.
import { $, esc, api } from "./util.js";
import { S, onRender, render, syncURL, prBase, repoName } from "./state.js";
import { impactPill, likelihoodPill, attLevel } from "./scores.js";
import { prepare, actions as diffActions } from "./diff.js";
import { syncComposer, focusComposer, actions as commentActions, onKeydown as composerKeydown } from "./comments.js";
import { reviewHTML, showDraft, actions as reviewActions } from "./review.js";
import { walkHTML, loadProgress, actions as walkActions, onKeydown as walkKeydown } from "./walkthrough.js";
import { treemapHTML, renderTreemap, actions as treemapActions } from "./treemap.js";
import { initPanel, renderPanel, panelOpen, closePanel, updateReviewButton } from "./panel.js";
import { initSidebar, loadList } from "./sidebar.js";
import { initTriage, triageURL } from "./triage.js";
import { initSettings, refreshSettings } from "./settings.js";
import * as budget from "./budget.js";

// TABS are the views of a triaged PR. mount runs after the tab's HTML is on
// the page.
const TABS = [
  { id: "review", label: "Review", html: reviewHTML },
  { id: "walk", label: "Walkthrough", html: walkHTML },
  { id: "map", label: "Code map", html: treemapHTML, mount: renderTreemap },
];

const actions = {
  ...diffActions, ...commentActions, ...reviewActions, ...walkActions, ...treemapActions,
  tab: (el) => { S.tab = el.dataset.tab; syncURL(); },
};

function prHeadHTML(r) {
  const pr = r.pr;
  return `
    <div class="pr-head">
      <h2><a href="${esc(pr.url)}" target="_blank" rel="noopener">${esc(pr.title)}</a> <span style="color:var(--muted);font-weight:400">#${pr.number}</span></h2>
      <div class="meta">${esc(repoName(pr))} · ${esc(pr.author)} · ${esc(pr.state.toLowerCase())} ·
        <code>${esc(pr.base_ref)}@${esc(pr.base_oid.slice(0, 8))}</code> ← <code>${esc(pr.head_ref)}@${esc(pr.head_oid.slice(0, 8))}</code> ·
        +${pr.additions}/−${pr.deletions} · classify <code>${esc(r.classifier)}</code> · summarize <code>${esc(r.summarizer)}</code>${r.summary_lang ? ` in ${esc(r.summary_lang)}` : ""} ·
        ${(r.duration_ms / 1000).toFixed(1)}s</div>
      ${r.impact || r.likelihood || r.attention ? `<div class="meta" style="margin-top:6px;display:flex;gap:6px;align-items:center;flex-wrap:wrap">
        ${impactPill(r.impact, "max impact")}${r.impact?.basis ? `<code>${esc(r.impact.basis)}</code>` : ""}
        ${likelihoodPill(r.likelihood, "max likelihood")}
        <span class="dz ${attLevel(r.attention)}" title="highest review attention">max attention ${r.attention}</span>
        ${r.codemap ? `<span title="code map build">map ${esc(r.codemap)}</span>` : ""}</div>` : ""}
    </div>`;
}

const tabsHTML = () => `<div class="tabs">${TABS.map((t) =>
  `<button class="${S.tab === t.id ? "on" : ""}" data-act="tab" data-tab="${t.id}">${t.label}</button>`).join("")}</div>`;

onRender(() => {
  const r = S.result;
  if (!r) return;
  const tab = TABS.find((t) => t.id === S.tab) || TABS[0];
  updateReviewButton();
  $("#main").innerHTML = prHeadHTML(r) + tabsHTML() + tab.html();
  tab.mount?.();
  focusComposer();
  if (panelOpen()) renderPanel();
});

async function showKey(key) {
  const r = await api(`/api/results/${encodeURIComponent(key)}`);
  budget.apply(r, S.cfg);
  S.result = r;
  Object.assign(S, { collapsed: new Set(), details: new Set(), diffOpen: {}, allHidden: false, above: {}, below: {}, files: {}, composer: null });
  S.tm.zoom = [];
  loadProgress();
  S.drafts = await api(`${prBase()}/drafts`).catch(() => []);
  prepare(r);
  syncURL();
  $("#url").value = r.pr.url;
  closePanel();
  render();
  refreshSettings();
  loadList();
}

$("#main").addEventListener("click", async (e) => {
  const el = e.target.closest("[data-act]");
  const handler = el && actions[el.dataset.act];
  if (!handler) return;
  syncComposer();
  if ((await handler(el, e)) !== false) render();
});
$("#main").addEventListener("keydown", composerKeydown);
document.addEventListener("keydown", walkKeydown);

(async () => {
  S.cfg = await api("/api/config").catch(() => null);
  initSidebar(showKey);
  initPanel(showDraft);
  initTriage(showKey);
  initSettings(() => { if (S.result) { budget.apply(S.result, S.cfg); render(); } });
  await loadList();
  const q = new URLSearchParams(location.search);
  if (TABS.some((t) => t.id === q.get("tab"))) S.tab = q.get("tab");
  if (q.get("key")) await showKey(q.get("key")).catch(() => {});
  else if (q.get("pr")) triageURL(q.get("pr"));
})();
