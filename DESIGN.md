# Holiday Lighting Designer

## Purpose

A local Go TUI for designing, simulating, testing, and publishing holiday and
seasonal lighting configuration. Home Assistant remains the only production
source of truth. The designer keeps local YAML working drafts and never adds
designer entities or runtime dependencies to HA. Those drafts may contain
designer-only color metadata, which is removed before diffing or publishing.

## Decisions

- Language: Go; one small executable, fast startup, simple distribution.
- UI: Bubble Tea TUI plus a server-rendered local web editor. The web command
  uses Go's standard HTTP and template packages, binds only to loopback, and
  adds no browser-side framework or production runtime.
- HA access: direct REST/WebSocket state and validation APIs; native YAML
  publishing uses a separate file transport because YAML-defined HA entries
  are not editable through storage config endpoints. SSH is the production
  transport; local filesystem transport is used for tests.
- Configuration: designer working YAML (`scenes.yaml`, `scripts.yaml`,
  `automations.yaml`, `input_select.yaml`, `colors.yaml`) projected to native
  HA YAML for review and publication.
- HA file ownership: one designer-owned native YAML file per configuration
  kind; unrelated Home Assistant entries remain in separate files.
- Dates: fixed or anchor-relative windows; generated automations evaluate time
  in HA's timezone.
- Production schedules: HA automations/scripts. The designer generates and
  updates those entries; it does not run schedules itself.
- Import: every entry in the designer-owned files by default; an explicit ref
  list may select a subset.

## Runtime flow

```text
HA import -> designer YAML draft -> TUI edit/simulate ----> native HA projection
                              \----> web edit/save/diff --/
                                                            |
                                                            v
                             publish diff -> stale check -> backup -> publish -> verify
                                                                              |
                                                                         rollback on error
```

The web editor keeps one in-memory draft behind a mutex. Mutation requests use
a per-process token and expected bundle hash, so stale browser tabs cannot
silently overwrite newer changes. Draft files change only through the explicit
Save action. The web interface does not preview Home Assistant; live preview
remains in the TUI. Web publishing is enabled only when an
imported baseline and HA credentials are configured, the in-memory draft has
been saved without external file changes, and the user types `PUBLISH`. It
delegates to the same stale-check, backup, validation, verification, and
rollback publisher as the TUI and CLI flows.

Preview captures exact available light state over WebSocket, applies a scene
only after an explicit confirmation, and restores the captured state with a
second command. Failed partial applications trigger automatic restoration.
Missing, unknown, or unavailable lights fail safely.

## Publish safety

1. Validate the draft at the trust boundary.
2. Re-import the entries recorded in the baseline.
3. Refuse publication if the live fingerprint differs from the imported
   baseline. The selected YAML draft carries a local sidecar fingerprint of
   the complete imported HA YAML set, so unrelated file changes also block.
4. Project both baseline and draft to native HA YAML, show that semantic diff,
   and publish the same projected data after confirmation.
5. Back up the imported state locally.
6. Update only changed or explicitly approved entries; never replace an entire
   HA category.
7. Reload the affected HA YAML domains.
8. Re-import and compare every affected entry.
9. Restore the exact pre-publish files and reload them if any update,
   validation, reload, or verification fails.

Scene deletion is two-stage: perform a read-only full YAML reference scan,
show affected scripts/automations, require those entries to be imported, remove
the scene from the draft, then separately confirm whether the HA scene itself
should be deleted.
