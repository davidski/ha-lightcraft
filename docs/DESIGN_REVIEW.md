# Web design review

Date: 2026-10-20. Basis: [draft web design standard](design-system.md).
Scope: the current web editor for a trusted, locally hosted household workflow.
This is a historical functional review, separate from the selected aesthetic
direction in [the design standard](design-system.md). Option A subsequently addressed
findings 1, 5, 6, and the label portion of 7; findings 2–4 were addressed
afterwards and verified in Chrome. The numbered findings below preserve the
original review evidence.

## Assessment

Keep the current visual direction and server-rendered structure. The dark
palette, catalog/editor layouts, native form controls, inventory-backed choices,
and guarded publish/import flows fit the application. A redesign or component
framework is not justified by this review.

There are concrete improvements to make. Fix the invalid added-step default and
loss of submitted edits first, then improve error clarity and accessible control
behavior. Cosmetic consistency comes after those changes.

Priorities: P1 blocks a normal task or loses unsaved input; P2 materially improves
recovery, access, or orientation; P3 improves efficiency and consistency.

## Must fix

### 1. P1 — Added steps start with invalid brightness

**Evidence:** [`web.html:35`](../web.html#L35), the `step-template` brightness
input has `max="100"` and `value="255"`. The initial server-rendered step uses
100. Chrome reproduction: open Sequences and click “Add color step”; the new
brightness input reads 255 and its `validity.valid` is false. The server also
rejects values above 100 in [`web.go:710`](../web.go#L710).

**Impact:** Adding a step creates an invalid form. Even after selecting its
catalog color, the user must discover and repair brightness before saving.

**Smallest fix:** Change the template default to 100. Keep conversion to the
Home Assistant 0–255 representation on the server.

**Acceptance:** Add a second step in Chrome; its brightness is 100 and valid.
A focused template regression check must cover the added-step default, not
only the percentage limits or server conversion.

### 2. P1 — Validation failures discard color and sequence edits

**Evidence:** [`web.go:649–671`](../web.go#L649) retains a submitted form only
for schedules. Failed color/sequence saves redirect without the edited ID or
submitted values; [`web.go:439–442`](../web.go#L439) initializes blank/default
editors. For example, submit an edited color with x=0.8 and y=0.8, or a multi-step
sequence whose transition exceeds its hold. Those errors pass individual HTML
field checks but fail server validation.

**Impact:** The persisted draft remains safe, but the user loses unsaved names,
coordinates, or step edits and must reconstruct them. Editing an existing object
also lands on the new-object form after the failure.

**Smallest fix:** Render the submitted editor on validation failure, or extend
the existing schedule retention approach to colors and sequences. Preserve raw
invalid values and step order so the user can correct the actual mistake.
Keep session and stale-write guards intact.

**Acceptance:** Submit both examples and verify the error, object identity,
and every entered value remain visible. Correct the error and save once.
Add focused regression tests for failed color and multi-step sequence submissions.

## Should fix

### 3. P2 — Errors use success styling and sometimes inaccurate action copy

**Evidence:** [`web.html:33`](../web.html#L33) renders every message as the same
green `.notice` with `role="status"`. [`web.go:655`](../web.go#L655) prefixes
all mutation failures with “Could not update draft”, including failures reached
through import, publish, playback, and inventory handlers using `post`.
The inventory-unavailable message also uses the same notice path.

**Impact:** A failed publish or unavailable inventory can look like success;
“update draft” gives the wrong context for recovering from a live operation.

**Smallest fix:** Give messages an explicit outcome type and action-specific
copy. Use a distinct error treatment with text, appropriate announcement
semantics, and a recovery step. Keep success notices separate.

**Acceptance:** Check invalid draft, failed inventory refresh, and failed
publish messages against successful saves. Verify the operation named, visual
state, and announcement all agree. Use controlled failures, not production writes.

### 4. P2 — Step actions lack context and preview cannot be paused

**Evidence:** [`web.html:35`](../web.html#L35) repeats buttons named only ↑, ↓,
and Remove. Both initial and dynamically added steps use those names. The
preview timer in [`web.html:73`](../web.html#L73) automatically advances and
loops without a pause control or reduced-motion handling.

**Impact:** Users of assistive technology cannot reliably identify which step
an operation affects. A looping preview continues updating alongside the editor
without a way to stop it.

**Smallest fix:** Give each step operation a descriptive name including its
step number; keep names correct after add/move/remove. Disable moves at list
boundaries and preserve sensible focus after removal. Add pause/resume for
preview cycling and suppress animated transitions under reduced-motion settings.

**Acceptance:** Operate a three-step sequence entirely by keyboard, checking
names and focus after every operation. Pause the preview and confirm it stays
paused past a hold interval. Test reduced-motion settings.

### 5. P2 — Current item and keyboard focus need consistent treatment

**Evidence:** Navigation has active CSS classes but no `aria-current`
([`web.html:25–31`](../web.html#L25)). Color, sequence, and schedule catalog links
have no selected treatment. The explicit focus-visible rule is limited to
publish disclosures ([`web.html:15`](../web.html#L15)); other elements depend on
browser defaults. This does not establish that default focus is invisible.

**Impact:** In a long catalog, the user cannot identify the edited row from the
list. Current location and focus treatment are inconsistent across workspaces.

**Smallest fix:** Add selected-row styling and accessible selection semantics,
`aria-current="page"` to current navigation, and a shared visible focus rule.
Reuse the existing mint outline; do not remove native outlines without replacing them.

**Acceptance:** Open an object in each catalog and verify exactly one selected
row. Tab through navigation, catalog links, form controls, and disclosures in
Chrome; verify visible focus and current-page semantics.

## Nice to have

### 6. P3 — Empty preview consumes the top of the editor

**Evidence:** [`web.html:35`](../web.html#L35) reserves a 300px scrolling preview
slot even when no sequence is selected. Confirmed in the local desktop browser.

**Impact:** A short empty-state sentence occupies a large panel and pushes
creation controls down the page.

**Smallest fix:** Use a compact empty preview while preserving a predictable
selected preview layout. Check the existing fixed-height assertion in
[`web_test.go:724`](../web_test.go#L724) when changing that contract.

**Acceptance:** Compare empty and selected sequence views at desktop and narrow
widths; keep the editor reachable and preview content usable.

### 7. P3 — Normalize save labels and timing units

**Evidence:** Colors says “Update color”; other editors say “Update draft”.
Sequence timing uses “Seconds” and “Transition” ([`web.html:34–35`](../web.html#L34)).

**Impact:** The draft-only boundary and the meaning of the timing values are
less clear than they could be.

**Smallest fix:** Use “Update draft” consistently; label timing “Hold (seconds)”
and “Transition (seconds)” in both rendered steps and the added-step template.

**Acceptance:** Inspect all three editors and both initial/added steps. Saving
must still clearly report a local draft update.

## Verification work before standard adoption

Narrow rendering, meaningful control-border contrast, browser zoom, screen-reader
behavior, and populated preview layout remain unverified. Treat these as checks,
not confirmed defects. The preview's fixed minimum columns and schedule pairs
warrant specific inspection with long IDs at 320px and 760px. Measure contrast
before claiming WCAG conformance or prescribing palette changes.

## Evidence and boundaries

Source review covered `web.html`, related `web.go` page/mutation handlers,
`lighting.go` validation, and relevant `web_test.go` assertions. The earlier
local Docker browser inspection covered all six workspaces with an empty,
disconnected dataset, including switching the schedule form to WLED. This
review additionally reproduced finding 1 in Chrome using that checkout's image.
Findings 2–5 and 7 are source-confirmed; populated runtime reproduction was not
performed. Finding 6 was visually confirmed in the earlier inspection.

The live production URL was unreachable during the earlier inspection.
No authenticated production behavior, live lighting operation, or production
publication was tested. No application code was changed and no application test
suite was run for this documentation-only change.

## Recommended order

Fix 1 and 2 first. Then address 3, followed by 4 and 5. Run the adoption checks
while those interaction changes are in progress. Finish with 6 and 7. Preserve
the existing visual system and publish safety invariants throughout.
