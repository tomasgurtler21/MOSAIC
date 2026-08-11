package descriptor

// classifier.go implements the Stage 2 FieldClassifier addition to
// internal/harness/descriptor:
//
//   FieldClass — the classification of one frontmatter key.
//   FieldClassifier — answers the classification question for one harness.
//   NewFieldClassifier — builds a classifier from a descriptor and a frontmatter plan.

import (
	"sort"

	"mosaic-deploy/internal/agentfields"
	"mosaic-deploy/internal/domain"
)

// FieldClass is the classification of one frontmatter key found in a deployed agent file.
type FieldClass string

const (
	// ClassMosaic — a key MOSAIC owns: generic vocabulary or a MOSAIC-only deployed field
	// under either its prefixed or legacy name.
	ClassMosaic FieldClass = "mosaic"
	// ClassHarness — a key the identified harness owns: its model key, its tools key, a key
	// its frontmatter plan sets, a key its spec drops, or a key named in its KeyOrder.
	//
	// "Owns" here means "is known to the harness", which is the FR-3 question: a key in this
	// class is not unknown and is therefore not stripped as unknown. It does NOT mean "should
	// be removed from a promoted generic file" — see DropCandidateKeys for that separate
	// question and why the two sets differ.
	ClassHarness FieldClass = "harness"
	// ClassDivertedTool — a key a ToolMapping DestField destination declares.
	ClassDivertedTool FieldClass = "diverted-tool"
	// ClassUnknown — anything else. Stripped per FR-3.
	ClassUnknown FieldClass = "unknown"
)

// FieldClassifier answers the classification question for one harness. Construct it once
// per file and reuse it for every key.
type FieldClassifier struct {
	// divertedFields maps a DestField destination key to its DivertedToolField declaration.
	// Used for ClassDivertedTool classification and DivertedField lookup.
	divertedFields map[string]DivertedToolField

	// harnessKeys is the set of keys classified ClassHarness (known to the harness).
	// MOSAIC keys and ClassDivertedTool keys are excluded — the precedence rules mean
	// they never reach ClassHarness. See Classify for the authoritative precedence.
	harnessKeys map[string]bool

	// dropCandidates is the subset of harnessKeys that names fields the harness ADDS to a
	// deployed file: ModelKey, ToolsKey, and plan Set keys. FrontmatterSpec.Drop and
	// FrontmatterPlan.Remove keys are excluded (AD-3).
	dropCandidates map[string]bool
}

// NewFieldClassifier builds a classifier from a harness's descriptor and the frontmatter
// plan it produced for the agent in question. Pass the zero FrontmatterPlan when no plan
// is available; the classifier then relies on descriptor-declared keys alone.
//
// Pure. Does not call module.Frontmatter itself — the caller supplies the plan, keeping
// the classifier free of the HarnessModule error path.
func NewFieldClassifier(d *domain.HarnessDescriptor, plan domain.FrontmatterPlan) FieldClassifier {
	c := FieldClassifier{
		divertedFields: make(map[string]DivertedToolField),
		harnessKeys:    make(map[string]bool),
		dropCandidates: make(map[string]bool),
	}

	// Build the diverted-field map first: it takes precedence over ClassHarness.
	for _, f := range DivertedToolFields(d) {
		c.divertedFields[f.Key] = f
	}

	// addHarness adds a key to harnessKeys if it is not empty, not a MOSAIC key (ClassMosaic
	// beats ClassHarness), and not a diverted-tool key (ClassDivertedTool beats ClassHarness).
	addHarness := func(k string) {
		if k == "" {
			return
		}
		if agentfields.IsKnownMosaicKey(k) {
			return
		}
		if _, isDiverted := c.divertedFields[k]; isDiverted {
			return
		}
		c.harnessKeys[k] = true
	}

	// addDropCandidate marks a key as a harness-added field (drop candidate). It applies the
	// same exclusions as addHarness: MOSAIC keys and diverted-tool keys are skipped.
	addDropCandidate := func(k string) {
		if k == "" {
			return
		}
		if agentfields.IsKnownMosaicKey(k) {
			return
		}
		if _, isDiverted := c.divertedFields[k]; isDiverted {
			return
		}
		c.dropCandidates[k] = true
	}

	// ModelKey and ToolsKey: the harness adds these to the deployed file.
	if k := d.Frontmatter.ModelKey; k != "" {
		addHarness(k)
		addDropCandidate(k)
	}
	if k := d.Frontmatter.ToolsKey; k != "" {
		addHarness(k)
		addDropCandidate(k)
	}

	// KeyOrder entries: the harness controls ordering of these keys and therefore knows them.
	for _, k := range d.Frontmatter.KeyOrder {
		addHarness(k)
	}

	// Spec.Drop entries: the harness removes these generic keys at deploy time. They are known
	// to the harness (FR-3) but the harness does NOT add them — they are generic fields being
	// removed. So they go into harnessKeys but NOT into dropCandidates (AD-3).
	for _, k := range d.Frontmatter.Drop {
		addHarness(k)
	}

	// Plan.Set keys: the harness adds these to the deployed file. Both known and candidates.
	setKeys := make(map[string]bool)
	for _, field := range plan.Set {
		if field.Key != "" {
			setKeys[field.Key] = true
			addHarness(field.Key)
			addDropCandidate(field.Key)
		}
	}

	// Plan.Remove keys: the harness removes these generic keys. Known to the harness (FR-3)
	// but not drop candidates — UNLESS the key also appears in plan.Set, in which case the
	// harness both adds and removes it (for different agent variants), and it IS a candidate.
	for _, k := range plan.Remove {
		if k != "" {
			addHarness(k)
			if setKeys[k] {
				addDropCandidate(k)
			}
		}
	}

	return c
}

// Classify returns the class of one frontmatter key. Precedence when a key would match
// more than one class: ClassMosaic, then ClassDivertedTool, then ClassHarness, then
// ClassUnknown. A MOSAIC-owned key therefore never classifies as harness-owned, which is
// what protects the AD-4 protected generic keys.
func (c FieldClassifier) Classify(key string) FieldClass {
	if agentfields.IsKnownMosaicKey(key) {
		return ClassMosaic
	}
	if _, ok := c.divertedFields[key]; ok {
		return ClassDivertedTool
	}
	if c.harnessKeys[key] {
		return ClassHarness
	}
	return ClassUnknown
}

// DivertedField returns the diverted-field declaration for key when
// Classify(key) == ClassDivertedTool.
func (c FieldClassifier) DivertedField(key string) (DivertedToolField, bool) {
	f, ok := c.divertedFields[key]
	return f, ok
}

// HarnessKeys returns every key classified ClassHarness, sorted. This is the
// "known to the harness" set and answers the FR-3 question only: a key in this set is not
// unknown and is not stripped as unknown.
//
// Do NOT use this set as a drop set. It includes the keys the harness's FrontmatterSpec.Drop
// and FrontmatterPlan.Remove declare, which name GENERIC fields the harness removes at deploy
// time. Use DropCandidateKeys for any removal decision.
func (c FieldClassifier) HarnessKeys() []string {
	result := make([]string, 0, len(c.harnessKeys))
	for k := range c.harnessKeys {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// DropCandidateKeys returns the subset of HarnessKeys that names fields the harness ADDS to a
// deployed file, sorted: the descriptor's ModelKey and ToolsKey, and the Key of every
// FrontmatterPlan.Set entry.
//
// Keys contributed solely by FrontmatterSpec.Drop or FrontmatterPlan.Remove are excluded.
// Those name generic fields the harness removes at deploy time, which is exactly what promote
// is trying to restore, not remove again (AD-3). A key that appears in both Set and Drop is
// included, because the harness does add it.
//
// This is the set a removal decision derives from. Keys in promoteProtectedGenericKeys remain
// the caller's responsibility to exclude on top of this (AD-4).
func (c FieldClassifier) DropCandidateKeys() []string {
	result := make([]string, 0, len(c.dropCandidates))
	for k := range c.dropCandidates {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
