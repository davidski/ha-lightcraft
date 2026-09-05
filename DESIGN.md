# Holiday Lighting Designer

## Purpose

A local Go TUI for designing, simulating, testing, and publishing holiday and
seasonal lighting configuration. Home Assistant remains the only production
source of truth. The designer keeps local native-HA-YAML drafts and never adds
designer entities, metadata, or runtime dependencies to HA.

## Decisions

- Language: Go; one small executable, fast startup, simple distribution.
- UI: Bubble Tea TUI.
- HA access: direct REST/WebSocket state and validation APIs; native YAML
  publishing uses a separate file transport because YAML-defined HA entries
  are not editable through storage config endpoints. SSH is the production
  transport; local filesystem transport is used for tests.
- Configuration: native HA YAML drafts (`scenes.yaml`, `scripts.yaml`,
  `automations.yaml`, `input_select.yaml`).
- HA file ownership: one designer-owned native YAML file per configuration
  kind; unrelated Home Assistant entries remain in separate files.
- Dates: fixed or anchor-relative windows; generated automations evaluate time
  in HA's timezone.
- Production schedules: HA automations/scripts. The designer generates and
  updates those entries; it does not run schedules itself.
- Initial import: only explicitly requested scene/script/automation/helper
  references. This limits blast radius and avoids importing unrelated HA data.

## Runtime flow

```text
HA import -> local YAML draft -> TUI edit/simulate -> native-YAML diff
                                      |
                                      v
                           stale check -> backup -> publish -> verify
                                                        |
                                                   rollback on error
```

Preview captures exact available light state over WebSocket, applies a scene
only after an explicit confirmation, and restores the captured state with a
second command. Failed partial applications trigger automatic restoration.
Missing, unknown, or unavailable lights fail safely.

## Publish safety

1. Validate the draft at the trust boundary.
2. Re-import the originally selected HA entries.
3. Refuse publication if the live fingerprint differs from the imported
   baseline. The selected YAML draft carries a local sidecar fingerprint of
   the complete imported HA YAML set, so unrelated file changes also block.
4. Show semantic changes in native-YAML form and require confirmation.
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
