# Holiday Lighting Designer

Go TUI for designing and publishing Home Assistant holiday lighting scenes,
scripts, and schedules.

## Drafts

Drafts are created locally by importing every entry in the designer-owned Home
Assistant files configured in `config.yaml`.

Run the TUI against an existing draft:

```sh
go run . --draft drafts/holiday --config config.yaml
```

Or use the Just recipes. The default target lists available commands:

```sh
just
just test
just build
just check
```

Launch the local web editor and open it in your browser:

```sh
just web
```

The web server listens only on `127.0.0.1:8080`. Use `just web port="18080"`
to choose another port, or run `go run . web --draft drafts/holiday --config
config.yaml --port 18080`. Use `--open=false` when the browser should not open
automatically. The web interface edits Scenes, Sequences, Schedules, and Colors
in memory, shows the current YAML and diff, and writes the draft files only
when **Save draft files** is selected. Pass `--against DIR` to compare with an
imported baseline; without it, the diff is against the draft at server startup.
Publishing is available only with that imported baseline and the configured
Home Assistant credentials. For example, `just web port="18080"
against="drafts/holiday-baseline"`. The web app requires the draft to be saved,
shows the exact diff, and requires typing `PUBLISH` before it runs the existing
stale check, backup, validation, reload, verification, and rollback flow.

The `config.yaml` file defines the HA connection and which entries are relevant
to this project. The `files` section points each kind at its designer-owned
native HA YAML file. The importer saves only the selected `kind:id` refs in the
draft when an optional ref list is supplied; otherwise it imports every entry.
Color definitions are created automatically from imported XY/HS scenes and
stored only in the local draft's `colors.yaml`.

For native YAML-defined HA entries, use the SSH file transport (the REST
config endpoints are storage-scene APIs and cannot edit YAML scenes):

The host must provide the normal `ssh` client/key or agent authentication.
The configured SSH host uses the `svc-01` SSH host alias, so the user, key,
port, and other connection settings come from `~/.ssh/config` and its included
`config.d` files. The HA URL, config directory, and native YAML file paths are
also in `config.yaml`.

```sh
HOMEASSISTANT_TOKEN=... \
just import drafts/holiday
```

The app fingerprints parsed configuration, so YAML key ordering does not
create false changes. Tests:

```sh
just test
```

Publish only after reviewing the printed diff:

```sh
just publish drafts/holiday drafts/holiday-baseline
```

Pass another config file as the second argument to `just import`, or the third
argument to `just publish`, when working with a different HA project
configuration.

`HOMEASSISTANT_TOKEN` supplies the token used for HA validation and reload.

Live preview and publishing require a configured Home Assistant URL and token.
The application uses direct HA REST/WebSocket APIs for state, preview, and
configuration validation. Native YAML import/publish uses SSH and never adds
designer entities or metadata to HA.

TUI controls:

- `Tab`/`j`/`k`/`Enter`: focus workspace navigation or its active pane, navigate, and open the selected editor. On the dashboard, `←`/`→` or `1`/`2`/`3`/`4` select the Scenes, Sequences, Colors, and YAML Draft workspaces; `Enter` opens the selected entry.
- `n`/`e`: create or edit a scene; `t`: create a generated date-window automation (`relative` offsets or recurring `fixed` MM-DD dates).
- `c`: browse, add, and edit reusable XY colors. Press `p` in the color form for the keyboard-driven CIE 1931 `x,y` picker. Scene forms choose a catalog color; the color name is draft-only and is removed before HA publish.
- `h`: edit ordered holiday sequences. Use `a` to add a scene, `x` to remove one occurrence, `[`/`]` to reorder, and `n` to create a selector sequence.
- `b` syncs the managed `holiday_lights` script and `input_select.holiday` infrastructure from the live HA YAML; review its diff before publishing.
- `v`: simulate the selected scene in the TUI without touching HA.
- `i`: read-only inventory of HA lights, color modes, and effect counts.
- `p`: type `PREVIEW` to apply the selected scene to real lights; `r` restores the captured state.
- `d`: view the semantic native-YAML diff; `u`: type `PUBLISH` to validate, back up, publish, verify, and roll back on failure.
- `x`: type `DELETE` to remove a scene from the draft, then type `DELETE HA` for the second confirmation to remove it from HA.

For TUI publish and stale detection, start with `--against` pointing at the
imported baseline. The optional `--refs` list overrides the HA entries managed
by that baseline.
