# HA Lightcraft

## Purpose

A local Go TUI and web editor for designing, testing, and publishing holiday
and seasonal lighting sequences, schedules, and Home Assistant-controlled WLED
programs. Home Assistant remains the production source of truth. The designer
keeps local YAML working drafts and never adds designer entities or runtime
dependencies to HA. Those drafts may contain designer-only colors, which are
compiled into generated scripts.

## Decisions

- Language: Go; one small executable, fast startup, simple distribution.
- UI: Bubble Tea TUI plus a server-rendered local web editor. The web command
  uses Go's standard HTTP and template packages, binds only to loopback, and
  renders each view directly from its final template, without rewriting
  rendered panels. It adds no browser-side framework or production runtime.
  When fronted by a
  reverse proxy, it accepts a validated `X-Forwarded-Prefix` while the proxy
  strips that prefix before forwarding; generated links and redirects retain
  the external path.
- HA access: direct REST/WebSocket state and validation APIs; native YAML
  publishing uses a separate file transport because YAML-defined HA entries
  are not editable through storage config endpoints. SSH is the production
  transport; local filesystem transport is used for tests. REST supplies
  configuration validation and reload, not a second publishing implementation.
- Configuration: Home Assistant connection, package, and legacy migration
  settings come from environment variables; the designer working Bundle is
  projected to one native HA package file with top-level
  `automation`, `input_select`, and `script` keys. The color catalog
  (`<draft-root>/colors.yaml`) remains outside that package.
- Color catalog: `<draft-root>/colors.yaml` lives beside the draft snapshots,
  outside the `current` and `proposed` Home Assistant file trees.
- HA file ownership: one designer-owned native YAML package contains all three
  managed configuration kinds; unrelated Home Assistant entries remain in
  separate files.
- Dates: fixed or anchor-relative windows; generated automations evaluate time
  in HA's timezone. Date ranges are inclusive and may be annual or one-off.
  All-day assignments use the complete date window without daily start/stop
  times.
- Production schedules: HA automations/scripts. The designer generates and
  updates those entries; it does not run schedules itself.
- Assignment content: an assignment either runs a reusable color sequence
  against ordinary light targets, or activates a named WLED preset/playlist
  through an explicit HA light entity, select entity, and select option. WLED
  programs belong to assignments, never to color-sequence steps.
- WLED ownership: the WLED app owns effects, palettes, segments, speed,
  intensity, and preset/playlist definitions. The designer references those
  definitions through HA and does not call the WLED API or synchronize frames
  across devices.
- Import: pull the configured native HA package into `<data>/current` and keep
  the editable designer bundle as the package basename directly in
  `<data>/proposed`. For the default package, these are
  `data/current/ha_lightcraft.yaml` and `data/proposed/ha_lightcraft.yaml`;
  the configured `packages/` prefix is only the remote HA path. `--data DIR`
  selects the root; it defaults to `data`. Every entry in the designer-owned
  package is imported. Web startup also accepts an empty local workspace; its
  Publish view can run the same import flow, while publication remains disabled
  until an imported baseline exists.
- Migration: imports detect the legacy `holiday_lights` format and read its
  optional scene file transiently to convert it into color sequences. Scenes
  are never copied into `current` or saved as a modern draft file. Migration
  is supported by the CLI and web import flows.
- Color representation: sequence steps store and publish Home Assistant
  `xy_color`; RGB is derived only for readable swatches and previews. Older
  RGB-only sequence data is converted when imported.

## Runtime flow

```text
HA package -> `<data>/current` -> `<data>/proposed` -> TUI/web edit -> native HA projection
                 ^                         ^
                 |                         |
                 +------ web Import -------+
                                                            |
                                                            v
                             publish diff -> stale check -> backup -> publish -> verify
                                                                              |
                                                                         rollback on error
```

An empty local workspace is a valid web starting state; the web Publish view
offers a confirmation-protected import that refreshes both snapshots and keeps
the existing local color catalog. The current snapshot preserves the configured
HA package path for ordinary file diffs while local package drafts use the flat
layout described above. Web and
TUI edits write only to `<data>/proposed`;
`<data>/current` is the comparison baseline. The web editor keeps one
in-memory draft behind a mutex. Mutation requests use
a per-process token and expected bundle hash, so stale browser tabs cannot
silently overwrite newer changes. Draft files are updated when mutations are
submitted. The web and TUI share one inventory loader for Home Assistant states,
entity metadata, locations, and readiness/error reporting, with UI-specific
presentation of the results. They reuse the same schedule playback-control
and guarded publisher paths. Schedule editors select
targets from a native checkbox picker rather than accepting entity IDs as free
text. Web publishing is enabled only when an
imported baseline and HA credentials are configured, the local draft has no
external file changes, and the user types `PUBLISH`. It
delegates to the same stale-check, backup, validation, verification, and
rollback publisher as the TUI and CLI flows.

For a color-sequence assignment, the generated player obtains the sequence's
steps and cycles them against its light targets. For a WLED assignment, the
generated player turns on the configured WLED light, selects the configured
option on the configured HA select entity, and remains active so a preset or
playlist is not restarted on every controller check. The shared
`lighting_assignments` controller starts eligible players at Home Assistant
start, on its five-minute time pattern, and after automation reload; the same
eligibility check stops players outside their inclusive date/time window. A
scheduled start or stop can therefore take up to five minutes to take effect.
An all-day WLED assignment therefore supports a controller such as the floating
string running continuously during a holiday period while Hue assignments use
shorter nightly windows.

Stopping a player always follows the assignment's `off` or `leave` behavior.
Pause stops the player; resume lets the controller apply the normal date and
time rules again. WLED and color
assignments participate in the same resource-overlap checks, including a WLED
select entity, so two assignments cannot contend for the same managed resource.

## Compatibility and ownership

Existing color-sequence assignments retain their data shape and generated
behavior. WLED assignments are additive and require a valid `light.*` entity,
`select.*` entity, and currently available select option when HA inventory is
available. Legacy `holiday_lights` imports still convert color scenes
transiently; they do not create WLED programs or copy scenes into the modern
data tree. Native YAML import, diff, and publish continue to use the configured
HA package path and guarded publisher.

The web Publish workspace shows the configured Home Assistant package in full
beside its publish changes. There is no file picker or separate in-memory save
step: editor updates persist to the proposed draft immediately. The local color
catalog remains outside that view and is edited through the Colors workspace.

Sequence previews are draft-only views. They take light targets from schedules
that reference the sequence and cycle each defined color step by its hold time;
they never call Home Assistant or change light state. WLED assignments are not
expanded into previews: their effects, palettes, segments, speed, intensity,
and preset/playlist definitions remain owned and previewed by the WLED app.

## Publish safety

1. Validate the draft at the trust boundary.
2. Re-import the entries recorded in the baseline and compare them with the
   pulled source snapshot.
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

The assignment trust boundary rejects mixed content modes, malformed HA entity
domains, and overlapping managed resources. Connected TUI/web authoring
requires successful Home Assistant state and entity-registry inventory before
saving WLED assignments; a registry failure blocks the save rather than
broadening choices or downgrading validation. It checks the selected WLED light,
select entity, and option against the current
HA inventory. WLED pickers use retained entity-registry metadata: only
`platform: wled` color-capable lights are offered, and only `preset` or
`playlist` selectors on the same non-empty device (with matching config entry
IDs when both are present) are offered. Native import, offline editing, and native publication validate
managed YAML shape but do not have HA state inventory; imported option
references are preserved and may become stale until revalidated in a connected
editor. The generated WLED action is limited to Home Assistant service calls
for the selected light and option; no direct WLED network access is part of the
publish path.
