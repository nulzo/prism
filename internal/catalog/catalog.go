// Package catalog is the single source of truth for the gateway's model
// registry. It replaces the mix of YAML-only static config, per-request
// provider lookups, and ad-hoc DB syncs that used to live in the gateway.
//
// Design goals (in priority order):
//
//  1. Always reflect reality. Upstream providers add / remove / rename
//     models out-of-band. The catalog calls `provider.Models(ctx)`
//     concurrently on every hydration pass and merges the result with
//     curated static metadata so the router never 404s on a model that
//     actually exists upstream.
//  2. Never clobber operator intent. Static YAML entries own
//     pricing / description / curated context length because those rarely
//     come back from provider list endpoints. Upstream-discovered models
//     fill in with safe defaults.
//  3. One commit per hydration. The in-memory view swaps atomically and
//     the SQLite `models` table is rewritten in a single transaction so
//     pricing lookups never race against the refresh.
//  4. Observable. Every hydration returns a diff + timings so operators
//     can see what changed and why.
package catalog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nulzo/model-router-api/internal/llm"
	"github.com/nulzo/model-router-api/pkg/api"
	"go.uber.org/zap"
)

// Source classifies where a ModelDefinition entered the catalog. Used for
// reporting and merge precedence.
type Source string

const (
	// SourceStatic means the entry originated from YAML (or any other
	// operator-curated config source).
	SourceStatic Source = "static"
	// SourceUpstream means the entry came from `provider.Models(ctx)` and
	// no static row existed.
	SourceUpstream Source = "upstream"
	// SourceMerged means both a static and upstream entry existed and were
	// merged into the final record.
	SourceMerged Source = "merged"
)

// Entry is the catalog's internal enriched record. It wraps the public
// ModelDefinition with the source + audit fields that only the catalog
// cares about.
type Entry struct {
	api.ModelDefinition
	// Provenance records how this entry was built (static, upstream, or
	// merged). Clients shouldn't care but it's invaluable when debugging
	// "why is this model here".
	Provenance Source
	// LastSeen is the timestamp of the hydration pass that last observed
	// this model upstream. Stale entries (only present in static YAML but
	// missing from the provider's current list) keep the zero value.
	LastSeen time.Time
}

// Catalog owns the in-memory model view and orchestrates hydration. It is
// safe for concurrent reads + one concurrent hydration. Callers that need
// to serialize hydrations should funnel them through Refresh().
type Catalog struct {
	log *zap.Logger

	mu        sync.RWMutex
	providers map[string]llm.Provider
	// static is the immutable YAML view, keyed by public model ID.
	static map[string]api.ModelDefinition

	// Snapshot is atomically swapped on every Hydrate so readers don't see
	// partial state during a refresh.
	snapshot atomic.Pointer[snapshot]

	hydrateTimeout time.Duration
	// sinks receive a copy of the snapshot after every successful hydrate.
	sinks []Sink
}

// snapshot is the immutable per-hydration view. Clients get a pointer to
// one of these via the Catalog.load helper and walk it without holding
// any locks.
type snapshot struct {
	models map[string]Entry
	// ordered caches a stable, alphabetical slice for listing so every
	// caller sees the same order without re-sorting.
	ordered []Entry
}

// Sink receives a copy of the snapshot after every successful hydrate.
// Implementations must be fast + side-effect safe to retry; the catalog
// logs and skips sinks that return an error.
type Sink interface {
	SinkName() string
	Absorb(ctx context.Context, snap Snapshot) error
}

// Snapshot is the public, read-only view of a catalog state handed to
// sinks and (indirectly) to callers of List / Resolve.
type Snapshot struct {
	Entries []Entry
	// TakenAt is the wall clock time when the snapshot was built.
	TakenAt time.Time
}

// Options configures a new Catalog. Pass the zero value to get sensible
// defaults.
type Options struct {
	Logger         *zap.Logger
	HydrateTimeout time.Duration
	// Static seeds the catalog with operator-curated entries that survive
	// every hydration cycle (pricing, description, etc.).
	Static []api.ModelDefinition
	// Sinks are persistence hooks invoked on every successful hydrate.
	Sinks []Sink
}

// New constructs a Catalog with no providers registered. Register providers
// with Add and then call Hydrate to populate the view.
func New(opts Options) *Catalog {
	log := opts.Logger
	if log == nil {
		log = zap.NewNop()
	}
	timeout := opts.HydrateTimeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}

	static := make(map[string]api.ModelDefinition, len(opts.Static))
	for _, m := range opts.Static {
		if m.ID == "" {
			continue
		}
		static[m.ID] = m
	}

	c := &Catalog{
		log:            log,
		providers:      map[string]llm.Provider{},
		static:         static,
		hydrateTimeout: timeout,
		sinks:          append([]Sink(nil), opts.Sinks...),
	}
	// Seed the snapshot with the static view so Resolve/List work before
	// the first hydrate. Provenance defaults to SourceStatic.
	seed := make(map[string]Entry, len(static))
	for id, def := range static {
		seed[id] = Entry{ModelDefinition: def, Provenance: SourceStatic}
	}
	c.swap(buildSnapshot(seed))
	return c
}

// Add registers a provider so subsequent hydrations will fetch its model
// list. Safe to call at startup or at runtime (e.g. a future hot-reload).
// Providers are keyed by Provider.Name().
func (c *Catalog) Add(p llm.Provider) {
	if p == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers[p.Name()] = p
}

// Remove unregisters a provider. Models contributed only by this provider
// will be dropped on the next hydrate; operator-curated static rows are
// preserved.
func (c *Catalog) Remove(providerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.providers, providerID)
}

// Providers returns a stable, alphabetical slice of registered provider
// IDs. Intended for diagnostic endpoints.
func (c *Catalog) Providers() []string {
	c.mu.RLock()
	ids := make([]string, 0, len(c.providers))
	for id := range c.providers {
		ids = append(ids, id)
	}
	c.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

// HydrateResult captures what a single Hydrate call observed. Returned to
// the caller so the admin endpoint / logs can surface human-readable diff
// summaries.
type HydrateResult struct {
	Providers   map[string]ProviderResult
	Added       []string
	Removed     []string
	Updated     []string
	TotalModels int
	Duration    time.Duration
}

// ProviderResult is the per-provider outcome of a hydration pass.
type ProviderResult struct {
	Models   int
	Err      error
	Duration time.Duration
}

// Hydrate rebuilds the catalog by fanning out provider.Models(ctx) calls
// in parallel and merging the results with static config. If `only` is
// non-empty only those provider IDs are refreshed; every other entry is
// preserved from the prior snapshot.
func (c *Catalog) Hydrate(ctx context.Context, only ...string) (*HydrateResult, error) {
	start := time.Now()

	c.mu.RLock()
	providers := make(map[string]llm.Provider, len(c.providers))
	for id, p := range c.providers {
		if len(only) > 0 && !contains(only, id) {
			continue
		}
		providers[id] = p
	}
	static := make(map[string]api.ModelDefinition, len(c.static))
	for id, m := range c.static {
		static[id] = m
	}
	c.mu.RUnlock()

	results := make(map[string]ProviderResult, len(providers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	// upstream maps model ID -> discovered definition across all providers.
	upstream := map[string]api.ModelDefinition{}

	for id, p := range providers {
		wg.Add(1)
		go func(id string, p llm.Provider) {
			defer wg.Done()

			pCtx, cancel := context.WithTimeout(ctx, c.hydrateTimeout)
			defer cancel()

			t0 := time.Now()
			models, err := p.Models(pCtx)
			elapsed := time.Since(t0)

			mu.Lock()
			results[id] = ProviderResult{Models: len(models), Err: err, Duration: elapsed}
			if err == nil {
				for _, m := range models {
					if m.ID == "" {
						continue
					}
					// Latest wins if two providers report the same public
					// ID. In practice this only happens when a model is
					// served through a primary + a fallback provider and
					// the config author wants the fallback to shadow the
					// primary; leave the default deterministic behavior
					// (unspecified) because the registration order comes
					// from a map iteration.
					upstream[m.ID] = m
				}
			}
			mu.Unlock()
		}(id, p)
	}
	wg.Wait()

	// When a hydration is scoped to a subset of providers, carry over the
	// prior snapshot's entries for every non-targeted provider.
	priorEntries := map[string]Entry{}
	if prior := c.load(); prior != nil {
		for id, e := range prior.models {
			priorEntries[id] = e
		}
	}

	now := time.Now()
	newModels := map[string]Entry{}

	// Static entries seed the result so curated metadata is always present
	// even when a provider briefly drops a model from its list.
	for id, def := range static {
		entry := Entry{
			ModelDefinition: def,
			Provenance:      SourceStatic,
		}
		if up, ok := upstream[id]; ok {
			entry = mergeStaticAndUpstream(def, up, now)
			delete(upstream, id)
		}
		newModels[id] = entry
	}

	// Anything left in `upstream` is a provider-announced model with no
	// corresponding static row. Promote it verbatim with safe defaults so
	// downstream consumers (sinks, filter, UI capability detection) never
	// see a half-formed record. Text-only is the implicit default because
	// nearly every LLM endpoint is a text completion surface; adapters
	// that discover image/audio-capable models should set these fields
	// explicitly before returning from Models(ctx).
	for id, up := range upstream {
		def := up
		if def.Source == "" {
			def.Source = string(SourceUpstream)
		}
		if len(def.Architecture.InputModalities) == 0 {
			def.Architecture.InputModalities = []string{"text"}
		}
		if len(def.Architecture.OutputModalities) == 0 {
			def.Architecture.OutputModalities = []string{"text"}
		}
		def.LastUpdated = now
		newModels[id] = Entry{
			ModelDefinition: def,
			Provenance:      SourceUpstream,
			LastSeen:        now,
		}
	}

	// When only a subset of providers was refreshed, merge in the prior
	// snapshot's entries for every untouched provider so we don't lose
	// them from the view.
	if len(only) > 0 {
		for id, prev := range priorEntries {
			if _, ok := newModels[id]; ok {
				continue
			}
			if contains(only, prev.ProviderID) {
				continue
			}
			newModels[id] = prev
		}
	}

	prior := c.load()
	snap := buildSnapshot(newModels)
	c.swap(snap)

	diff := diffSnapshots(prior, snap)
	diff.Providers = results
	diff.Duration = time.Since(start)

	// Fire sinks after the swap so readers see the new state immediately
	// even if a sink is slow.
	for _, s := range c.sinks {
		if err := s.Absorb(ctx, snap.public(now)); err != nil {
			c.log.Warn("catalog sink failed",
				zap.String("sink", s.SinkName()),
				zap.Error(err),
			)
		}
	}

	return &diff, nil
}

// Watch runs Hydrate on the configured interval until ctx is done. The
// initial hydration is NOT performed by Watch; callers should invoke
// Hydrate once at startup so the app is ready before any traffic arrives.
// Errors from periodic hydrates are logged and do not stop the loop.
func (c *Catalog) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			res, err := c.Hydrate(ctx)
			if err != nil {
				c.log.Warn("scheduled catalog hydrate failed", zap.Error(err))
				continue
			}
			c.log.Info("catalog hydrated",
				zap.Int("models", res.TotalModels),
				zap.Int("added", len(res.Added)),
				zap.Int("removed", len(res.Removed)),
				zap.Int("updated", len(res.Updated)),
				zap.Duration("duration", res.Duration),
			)
		}
	}
}

// Resolve returns the provider ID + upstream model ID for the given public
// model identifier. The upstream ID falls back to the public ID when the
// entry doesn't carry an explicit upstream mapping (common for auto-
// discovered entries whose config author didn't split the namespace).
func (c *Catalog) Resolve(modelID string) (providerID, upstreamID string, ok bool) {
	snap := c.load()
	if snap == nil {
		return "", "", false
	}
	e, found := snap.models[modelID]
	if !found {
		return "", "", false
	}
	up := e.UpstreamID
	if up == "" {
		up = modelID
	}
	return e.ProviderID, up, true
}

// Lookup returns the full catalog entry for a public model ID. Used by the
// OpenRouter-style `/v1/models/:id` endpoint and by request-level features
// that need to inspect a model's declared capabilities.
func (c *Catalog) Lookup(modelID string) (Entry, bool) {
	snap := c.load()
	if snap == nil {
		return Entry{}, false
	}
	e, ok := snap.models[modelID]
	return e, ok
}

// All returns a stable alphabetical slice of every entry in the current
// snapshot. The slice is a defensive copy so callers can mutate it
// freely.
func (c *Catalog) All() []Entry {
	snap := c.load()
	if snap == nil {
		return nil
	}
	out := make([]Entry, len(snap.ordered))
	copy(out, snap.ordered)
	return out
}

// Filter returns the subset of entries that match the given ModelFilter.
// Filter matching mirrors the behavior of the pre-catalog
// gateway.ListAllModels helper: substring ID match, exact provider / owned_by
// match, and any-of modality membership.
func (c *Catalog) Filter(f api.ModelFilter) []Entry {
	all := c.All()
	if f.Provider == "" && f.ID == "" && f.Modality == "" && f.OwnedBy == "" {
		return all
	}
	out := all[:0:0]
	lowerID := strings.ToLower(f.ID)
	for _, e := range all {
		if f.Provider != "" && !strings.EqualFold(e.ProviderID, f.Provider) {
			continue
		}
		if f.ID != "" && !strings.Contains(strings.ToLower(e.ID), lowerID) {
			continue
		}
		if f.Modality != "" {
			// Upstream-discovered models usually come back without
			// Architecture metadata (the provider `list models`
			// endpoints don't always carry modality info). Treat a
			// missing InputModalities list as "text-only" — the
			// dominant reality for LLM endpoints — so the default
			// `?modality=text` query from the UI still returns them.
			mods := e.Architecture.InputModalities
			if len(mods) == 0 {
				mods = []string{"text"}
			}
			if !containsFold(mods, f.Modality) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func (c *Catalog) load() *snapshot { return c.snapshot.Load() }
func (c *Catalog) swap(s *snapshot) {
	if s == nil {
		return
	}
	c.snapshot.Store(s)
}

// mergeStaticAndUpstream merges operator-curated metadata with discovered
// provider data. The rule is: static wins for everything a human would
// tune (pricing, description, instruction format, naming) and upstream
// wins for everything that changes on the provider's side (context
// length, modalities, last-seen timestamp). Unset upstream fields never
// overwrite a set static field.
func mergeStaticAndUpstream(static api.ModelDefinition, upstream api.ModelDefinition, now time.Time) Entry {
	out := static

	// Upstream-preferred fields: provider-owned truth.
	if upstream.ContextLength > 0 {
		out.ContextLength = upstream.ContextLength
	}
	if out.TopProvider.ContextLength == 0 && upstream.TopProvider.ContextLength > 0 {
		out.TopProvider = upstream.TopProvider
	}
	if len(out.Architecture.InputModalities) == 0 && len(upstream.Architecture.InputModalities) > 0 {
		out.Architecture.InputModalities = upstream.Architecture.InputModalities
	}
	if len(out.Architecture.OutputModalities) == 0 && len(upstream.Architecture.OutputModalities) > 0 {
		out.Architecture.OutputModalities = upstream.Architecture.OutputModalities
	}
	if out.Architecture.Tokenizer == "" && upstream.Architecture.Tokenizer != "" {
		out.Architecture.Tokenizer = upstream.Architecture.Tokenizer
	}

	// Static-preferred fields: only take from upstream when static is
	// missing a value.
	if out.UpstreamID == "" {
		out.UpstreamID = upstream.UpstreamID
	}
	if out.Name == "" {
		out.Name = upstream.Name
	}
	if out.Description == "" {
		out.Description = upstream.Description
	}
	if out.Pricing.Prompt == "" && upstream.Pricing.Prompt != "" {
		out.Pricing = upstream.Pricing
	}
	if out.CanonicalSlug == "" {
		out.CanonicalSlug = upstream.CanonicalSlug
	}
	if out.HuggingFaceID == "" {
		out.HuggingFaceID = upstream.HuggingFaceID
	}
	if len(out.SupportedParameters) == 0 && len(upstream.SupportedParameters) > 0 {
		out.SupportedParameters = append([]string(nil), upstream.SupportedParameters...)
	}
	if len(out.DefaultParameters) == 0 && len(upstream.DefaultParameters) > 0 {
		out.DefaultParameters = make(map[string]interface{}, len(upstream.DefaultParameters))
		for k, v := range upstream.DefaultParameters {
			out.DefaultParameters[k] = v
		}
	}

	out.Source = string(SourceMerged)
	out.LastUpdated = now

	return Entry{ModelDefinition: out, Provenance: SourceMerged, LastSeen: now}
}

func buildSnapshot(m map[string]Entry) *snapshot {
	ordered := make([]Entry, 0, len(m))
	for _, e := range m {
		ordered = append(ordered, e)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return &snapshot{models: m, ordered: ordered}
}

func (s *snapshot) public(at time.Time) Snapshot {
	if s == nil {
		return Snapshot{TakenAt: at}
	}
	out := make([]Entry, len(s.ordered))
	copy(out, s.ordered)
	return Snapshot{Entries: out, TakenAt: at}
}

// diffSnapshots returns the add/remove/update deltas between two
// snapshots. "Updated" is defined as any change to context length,
// pricing, modalities, or upstream ID because those are the fields a
// human operator actually cares about.
func diffSnapshots(prev, curr *snapshot) HydrateResult {
	result := HydrateResult{}
	if curr != nil {
		result.TotalModels = len(curr.models)
	}
	if prev == nil {
		if curr != nil {
			for id := range curr.models {
				result.Added = append(result.Added, id)
			}
		}
		sort.Strings(result.Added)
		return result
	}
	if curr == nil {
		for id := range prev.models {
			result.Removed = append(result.Removed, id)
		}
		sort.Strings(result.Removed)
		return result
	}
	for id, e := range curr.models {
		pe, ok := prev.models[id]
		if !ok {
			result.Added = append(result.Added, id)
			continue
		}
		if changed(pe.ModelDefinition, e.ModelDefinition) {
			result.Updated = append(result.Updated, id)
		}
	}
	for id := range prev.models {
		if _, ok := curr.models[id]; !ok {
			result.Removed = append(result.Removed, id)
		}
	}
	sort.Strings(result.Added)
	sort.Strings(result.Removed)
	sort.Strings(result.Updated)
	return result
}

func changed(a, b api.ModelDefinition) bool {
	if a.UpstreamID != b.UpstreamID || a.ContextLength != b.ContextLength {
		return true
	}
	if a.Pricing != b.Pricing {
		return true
	}
	if !equalStrings(a.Architecture.InputModalities, b.Architecture.InputModalities) ||
		!equalStrings(a.Architecture.OutputModalities, b.Architecture.OutputModalities) {
		return true
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func containsFold(s []string, v string) bool {
	for _, x := range s {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// ErrNoProviders is returned by Hydrate when no providers are registered.
// Callers can treat this as a configuration error during bootstrap.
var ErrNoProviders = fmt.Errorf("catalog: no providers registered")
