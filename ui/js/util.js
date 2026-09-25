// Small helpers shared by every component.

export const $ = (s) => document.querySelector(s);
export const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

export const BUCKETS = ["human", "summary", "none"];
export const LABEL = { human: "human review", summary: "read summary", none: "no review" };

export async function api(path, opts) {
  const r = await fetch(path, opts);
  const body = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(body.error || r.statusText);
  return body;
}
export const postJSON = (url, v) => api(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(v) });

export function pills(c) {
  return BUCKETS.map((b) => `<span class="pill ${b}" title="${LABEL[b]}">${c?.[b] || 0}</span>`).join("");
}

// firstSentence trims long model text down to its opening sentence.
function firstSentence(t) {
  const m = String(t || "").match(/^[\s\S]*?[.!?](?=\s|$)/);
  return (m ? m[0] : String(t || "")).trim();
}

// headline is the one line shown per unit: the summarizer's headline, else
// the first sentence of its summary, else the classifier's reason.
export function headline(u) {
  return u.headline || u.decision.headline || firstSentence(u.summary) || firstSentence(u.decision.reason) || "(no description)";
}
