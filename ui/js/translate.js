// Summary language. PRs are reviewed in English; another language is
// translated by the server when a PR is opened, and cached there. The
// English text is kept so switching language, or back to English, needs
// no new triage.
import { postJSON } from "./util.js";
import { S, render } from "./state.js";
import { jobSettings, summaryLang } from "./settings.js";
import { syncComposer } from "./comments.js";

const english = new WeakMap(); // result -> {lang, units: id -> text}
let seq = 0;

const units = (r) => r.files.flatMap((f) => f.units || []);
const textOf = (u) => ({
  headline: u.headline, summary: u.summary, focus: u.focus && [...u.focus],
  issues: (u.issues || []).map((i) => ({ title: i.title, detail: i.detail, failure_scenario: i.failure_scenario })),
});

function put(u, t, keep) {
  const set = (o, k, v) => { if (v || !keep) o[k] = v; };
  set(u, "headline", t.headline);
  set(u, "summary", t.summary);
  if (t.focus && u.focus?.length === t.focus.length) t.focus.forEach((f, i) => set(u.focus, i, f));
  if (t.issues && (u.issues || []).length === t.issues.length) {
    t.issues.forEach((is, i) => { for (const k of ["title", "detail", "failure_scenario"]) set(u.issues[i], k, is[k]); });
  }
}

// restore puts the result's own text back.
function restore(r) {
  const en = english.get(r);
  if (!en) return;
  for (const u of units(r)) if (en.units[u.id]) put(u, en.units[u.id], false);
  r.summary_lang = en.lang;
  english.delete(r);
}

// translate shows the open result in the chosen summary language. English
// (or the result's own language) makes no request.
export async function translate() {
  const r = S.result;
  if (!r) return;
  const n = ++seq;
  const had = english.has(r) || r.translating || r.translate_error;
  restore(r);
  r.translating = r.translate_error = "";
  const lang = summaryLang();
  if (!lang || lang.toLowerCase() === "english" || lang.toLowerCase() === (r.summary_lang || "").toLowerCase()) {
    if (had) { syncComposer(); render(); }
    return;
  }
  r.translating = lang;
  syncComposer();
  render();
  try {
    const tr = await postJSON(`/api/results/${encodeURIComponent(r.key)}/translate`, { ...jobSettings(), summary_lang: lang });
    if (n !== seq || S.result !== r) return;
    if (tr.lang) {
      english.set(r, { lang: r.summary_lang, units: Object.fromEntries(units(r).map((u) => [u.id, textOf(u)])) });
      for (const u of units(r)) if (tr.units[u.id]) put(u, tr.units[u.id], true);
      r.summary_lang = tr.lang;
    }
  } catch (e) {
    if (n !== seq || S.result !== r) return;
    r.translate_error = `not translated to ${lang}: ${e.message}`;
  }
  r.translating = "";
  syncComposer();
  render();
}
