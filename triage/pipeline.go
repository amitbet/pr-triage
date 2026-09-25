package triage

import (
	"context"
	"sort"
	"sync"

	"github.com/amitbet/pr-triage/llm"
	"golang.org/x/sync/errgroup"
)

type Pipeline struct {
	Presorter   *Presorter
	Classifier  Classifier  // nil: every unit left after presort is "human"
	Summarizer  *Summarizer // nil: no summaries, review notes or issues
	CodeMap     *CodeMap    // nil: no impact scores, no file history and no tier moves
	Concurrency int
	// ReviewConcurrency limits the summarize stage; 0 uses Concurrency.
	// Review calls are slower (bigger model, repo tools), so they get
	// their own limit.
	ReviewConcurrency int
	// Progress, if set, is called as units finish in each LLM stage.
	Progress func(stage string, done, total int)
	// Warn, if set, gets problems that only degrade the run.
	Warn func(msg string)
}

type Report struct {
	Base  string  `json:"base"`
	Head  string  `json:"head"`
	Units []*Unit `json:"units"`
}

// Scores returns the highest code-map impact (nil without a map), the
// highest likelihood and the highest review attention. Units a rule skipped
// (generated, formatting) do not count: their impact shows up where the
// source changed.
func (r *Report) Scores() (*Impact, *Likelihood, int) {
	var d *Impact
	var l *Likelihood
	att := 0
	for _, u := range r.Units {
		skipped := u.Decision.Source == "rule" && u.Decision.Bucket == BucketNone
		if skipped {
			continue
		}
		if u.Impact.Known() && (d == nil || u.Impact.Score > d.Score) {
			d = u.Impact
		}
		if u.Likelihood != nil && (l == nil || u.Likelihood.Score > l.Score) {
			l = u.Likelihood
		}
		att = max(att, u.Attention)
	}
	return d, l, att
}

func (r *Report) Counts() map[Bucket]int {
	c := map[Bucket]int{}
	for _, u := range r.Units {
		c[u.Decision.Bucket]++
	}
	return c
}

// Run triages src and returns units sorted human → summary → none.
func (p *Pipeline) Run(ctx context.Context, src *Source) []*Unit {
	units := BuildUnits(src.Files, src.Content, p.Presorter.Policy.MaxUnitChars)
	rest := p.Presorter.Presort(units, src)
	setRelated(units)
	if p.CodeMap != nil {
		files := map[string]FileDiff{}
		for _, f := range src.Files {
			files[f.Path] = f
		}
		for _, u := range units {
			u.Impact = p.CodeMap.Assess(u, files[u.File], src.BaseContent)
		}
	}
	lc := newLikelihoodCtx(src, p.CodeMap)
	for _, u := range units {
		u.Likelihood = lc.assess(u, fileOf(src, u.File))
	}
	tiers := p.Presorter.Policy.Tiers
	maxChars := p.Presorter.Policy.MaxUnitChars

	p.each(ctx, "classify", p.Concurrency, rest, func(ctx context.Context, u *Unit) {
		if p.Classifier == nil {
			u.Decision = Decision{Bucket: BucketHuman, Source: "none", Reason: "no classifier configured"}
		} else {
			u.Decision = p.Classifier.Classify(ctx, u)
		}
	})
	// Rule-decided units get a score too, so every unit explains its bucket.
	for _, u := range units {
		tiers.prior(u, maxChars)
	}

	if p.Summarizer != nil {
		setReviewContext(units, src.BaseContent, p.Presorter.Policy.ReviewContextChars)
		sum := *p.Summarizer
		if sum.Tools && llm.SupportsWorkspace(sum.LLM) {
			// Without a workspace the review still runs, just without tools.
			ws, cleanup, err := reviewWorkspace(src)
			if err != nil && p.Warn != nil {
				p.Warn("review tools off: " + err.Error())
			}
			defer cleanup()
			sum.workspace = ws
		}
		var toSummarize []*Unit
		prev := map[*Unit]Bucket{}
		for _, u := range units {
			// Units placed in human before review get review notes, the
			// rest a summary and a second opinion.
			if tiers.reviewable(u) {
				toSummarize = append(toSummarize, u)
				prev[u] = u.Decision.Bucket
			}
		}
		limit := p.ReviewConcurrency
		if limit <= 0 {
			limit = p.Concurrency
		}
		p.each(ctx, "summarize", limit, toSummarize, sum.Summarize)
		for _, u := range toSummarize {
			tiers.afterReview(u, prev[u])
		}
	}

	SortByBucket(units)
	return units
}

// SortByBucket orders units human → summary → none, and by score inside
// a bucket (then risk), stable for ties.
func SortByBucket(units []*Unit) {
	total := func(u *Unit) int {
		if u.Score == nil {
			return 0
		}
		return u.Score.Total
	}
	sort.SliceStable(units, func(i, j int) bool {
		a, b := units[i].Decision.Bucket.rank(), units[j].Decision.Bucket.rank()
		if a != b {
			return a > b
		}
		if ti, tj := total(units[i]), total(units[j]); ti != tj {
			return ti > tj
		}
		return units[i].Risk() > units[j].Risk()
	})
}

func fileOf(src *Source, p string) FileDiff {
	for _, f := range src.Files {
		if f.Path == p {
			return f
		}
	}
	return FileDiff{Path: p}
}

func (p *Pipeline) each(ctx context.Context, stage string, limit int, units []*Unit, fn func(context.Context, *Unit)) {
	var mu sync.Mutex
	done := 0
	report := func() {
		if p.Progress != nil {
			p.Progress(stage, done, len(units))
		}
	}
	report()
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(max(1, limit))
	for _, u := range units {
		g.Go(func() error {
			fn(ctx, u)
			mu.Lock()
			done++
			report()
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
}

// setRelated gives each unit the IDs of the other units, capped so large
// PRs don't blow up every prompt.
func setRelated(units []*Unit) {
	const maxRelated = 60
	for _, u := range units {
		u.Related = u.Related[:0]
		for _, o := range units {
			if o != u && len(u.Related) < maxRelated {
				u.Related = append(u.Related, o.ID)
			}
		}
	}
}
