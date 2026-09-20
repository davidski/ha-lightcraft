# ![HA Lightcraft icon](favicon-transparent.png) HA Lightcraft

[![Docker](https://img.shields.io/badge/Docker-ghcr.io-2496ED?logo=docker&logoColor=white)](https://github.com/davidski/ha-lightcraft/pkgs/container/ha-lightcraft)
[![Home Assistant](https://img.shields.io/badge/Home%20Assistant-compatible-41BDF5?logo=homeassistant&logoColor=white)](https://www.home-assistant.io/)
[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](https://go.dev/)

Seasonal lighting designer for Home Assistant.

Go TUI and local web editor for designing and publishing Home Assistant
lighting color sequences, schedules, and WLED programs.

Licensed under the [MIT License](LICENSE).

## AI disclosure

Built with LLM coding agents as a hands-on Go learning project. This is still a
hobby project, so use it with caution.

## Getting started

### 1. Configure the environment

Download the [sample environment file](https://github.com/davidski/ha-lightcraft/blob/main/.env.sample)
and [sample `compose.yaml`](https://github.com/davidski/ha-lightcraft/blob/main/compose.yaml),
copy the environment file to `.env`, and update it with the Home Assistant URL,
SSH host and user, remote config directory, path to a private SSH key with
access to your Home Assistant host, and [Home Assistant API token](https://www.home-assistant.io/docs/authentication/)
for your environment.

Keep `.env` private as it now contains deployment settings and your secret HA token.

Create the host-side data and backup directories:

```sh
mkdir -p data backups
```

### 2. Initialize Home Assistant files

The CLI import initializes the local baseline and editable draft:

```sh
docker compose run --rm ha-lightcraft import
```

This writes the current in-use version to `data/current/ha_lightcraft.yaml` and
creates the editable version at `data/proposed/ha_lightcraft.yaml`.

The web editor can also start with an empty data directory. Open its Publish
view and use **Import from Home Assistant** to perform the same initialization.

### 3. Edit in the TUI or web editor

The TUI and web interfaces are equivalent. Choose whichever fits your
workflow/preferences.

#### TUI

Run the TUI:

```sh
docker compose run --rm ha-lightcraft tui
```

#### Web editor

Launch the local web editor:

```sh
docker compose run --rm --service-ports ha-lightcraft web
```

The web server listens on `127.0.0.1:8080` by default. The web interface edits
and persists color sequences, schedules, reusable colors, and WLED program
assignments. Schedule targets use pickers populated from the Home Assistant
inventory, with existing draft references retained while offline. It can pause
or resume published schedules, lists the configured Home Assistant package
beside its full contents and publish changes.

Use `web --listen ADDRESS` to intentionally bind another address.

## Running the application

### Supported environment variables:

- `HA_LIGHTCRAFT_PORT` (default `8080`): host port mapped to the container's web server.
- `HOMEASSISTANT_URL` (default empty): Home Assistant URL used for API access.
- `HOMEASSISTANT_TOKEN` (default empty): Home Assistant token for inventory, playback control, validation, and publishing.
- `HOMEASSISTANT_SSH_HOST` (default empty): SSH host used to transfer native Home Assistant YAML.
- `HOMEASSISTANT_SSH_USER` (default empty): SSH user used to transfer native Home Assistant YAML.
- `HOMEASSISTANT_SSH_KEY` (default empty): path to the private SSH key used for native YAML transfer. Compose reads this host file as a secret and mounts it into the container.
- `HOMEASSISTANT_CONFIG_DIR` (default empty): remote Home Assistant configuration directory, typically `/config`.
- `HOMEASSISTANT_PACKAGE` (default `packages/ha_lightcraft.yaml`): remote designer-owned Home Assistant package path.
- `HOMEASSISTANT_LEGACY_SCENES_FILE` (default empty): optional remote scene file used only when `import` migrates the old `holiday_lights` format.
- `WEB_ALLOWED_HOSTS` (default empty): comma-separated external hostnames accepted by the web editor in addition to loopback hosts. Use this when serving the editor through a reverse proxy, for example `cherry.woohouse.world`.
- `PUID` (default `1000`): numeric user ID used for files written to bind-mounted directories.
- `PGID` (default `1000`): numeric group ID used for files written to bind-mounted directories.

These Home Assistant variables are also read by direct CLI and `web` commands;
the corresponding command-line flags override their environment values.

### Hosting and security

The web app is intended for local hosting in a trusted environment. While
sub-path hosting via `X-Forwarded-Prefix` headers is supported, neither user
authentication nor HTTPS is provided. For reverse-proxy hosting, set
`WEB_ALLOWED_HOSTS` to the exact public hostname and configure the proxy to
authenticate requests and strip the external path prefix. Lightcraft continues
to accept loopback hosts by default and rejects other hostnames.

## Configuration and lighting model

### Configuration files

Color definitions are stored in `data/colors.yaml` and are compiled into
sequence steps when schedules are generated.

### Home Assistant package setup

Lightcraft uses a package file to load its automations, helpers, and scripts.
Add this to the existing `homeassistant:` block in Home Assistant's
`configuration.yaml`:

```yaml
homeassistant:
  packages:
    ha_lightcraft: !include packages/ha_lightcraft.yaml
```

Lightcraft writes `/config/packages/ha_lightcraft.yaml` in this shape:

```yaml
automation: []
input_select: {}
script: {}
```

The actual generated values will replace the empty collections upon publication.

The name of the package can overridden via the `HOMEASSISTANT_PACKAGE` envvar.

### Generated lighting

Each sequence is published as a Home Assistant script named
`script.lighting_sequence_<sequence_id>`. That script returns the sequence's
color steps; it does not directly control lights. Each schedule that uses a
sequence adds a separate `script.lighting_assignment_<schedule_id>` player,
an `input_select` status helper, and the shared `lighting_assignments` automation
that starts and stops scheduled players. A schedule has one of two content
modes:

- A color-sequence assignment runs one reusable sequence against ordinary light
  targets.
- A WLED assignment activates one named preset or playlist through an explicit
  Home Assistant light entity, select entity, and select option. WLED programs
  are assignment content, not color-sequence steps.

For example, run a given WLED string continuously over a holiday period by
creating an all-day WLED assignment for its preset or playlist, then create a
separate nightly color-sequence assignment for your Hue-style bulbs. Date
ranges include both their first and last dates. All-day assignments ignore
daily start and stop times while keeping the date range.

### Runtime behavior

At runtime, a WLED player turns on its light, selects the named option, and
stays active for the duration of the assignment. The shared controller checks
eligibility at Home Assistant start, every five minutes, and after automation
reload. Scheduled starts and stops can therefore take up to five minutes to
take effect. When an assignment stops, its configured `off` or `leave` behavior
applies; pause stops the player, and resume returns to the normal date and time
rules.

### WLED ownership

The designer references WLED programs through Home Assistant only. The WLED app
remains responsible for effects, palettes, segments, speed, intensity, and
preset/playlist definitions.

## Controls and editor details

### TUI controls

TUI controls:

- `Tab`: switch focus; `hjkl`/arrow keys navigate. On the dashboard, `1`/`2`/`3`/`4` select the Colors, Sequences, Schedules, and Publish workspaces, and `Enter` opens the selected entry.
- `a`: open the 14-day schedule agenda.
- `n`: create a sequence, schedule, or color from the corresponding workspace. In a lighting list, `Enter`/`e` edits the selected sequence or schedule.
- `s`: apply the active editor and save the proposed draft files.
- `p`: preview the selected sequence; in the schedule list, pause live playback. `r` resumes live playback.
- In the schedule editor, choose either a color-sequence assignment with light
  targets or a WLED assignment with a light, select entity, and named preset or
  playlist. The schedule editor makes that program type choice explicit, and its
  **all day** control disables the daily start/stop fields for continuous
  operation over the inclusive date range.
- In a color form, `p` opens the keyboard-driven CIE 1931 `x,y` picker.
- `i`: read-only inventory of HA lights, color modes, and effect counts.
- `d`: view the semantic native-YAML diff; `u`: type `PUBLISH` to validate, back up, publish, verify, and roll back on failure.
- `x`: type `DELETE` to remove the selected color or sequence. Schedules are retired by disabling them instead.
- `Esc`: cancel or go back. `q` quits, prompting first when unsaved changes exist.

### Web editor details

The web schedule editor exposes the same two assignment modes, inventory-backed
WLED light/select/option choices scoped to the selected WLED controller. A
connected save requires both state and entity-registry inventory; an unavailable
registry blocks WLED saves.  Offline imported references remain visible and
structurally editable. The editor also supports inclusive annual or one-off
dates, and the all-day setting. Color-sequence previews remain color-only;
use the WLED app to review WLED effects and program definitions.

## Development

These recipes are for development and verification rather than everyday use:

```sh
just              # list available recipes
just test         # run unit tests
just lint         # run project-lint checks
just build        # build the binary
just check        # run tests, vet, lint, race checks, and build
```

Install `prek` and `golangci-lint`, then run `prek install` to enable the
pre-commit hook. `prek run --all-files` runs the hook across the repository.

## See Also

For general-purpose seasonal scheduling in Home Assistant, see
[ha-scheduler](https://github.com/Smiley73/ha-scheduler). This project focuses
on lighting-specific design: reusable custom colors, sequenced brightness and
transition steps, controlling WLED programs, and publishing those programs to
Home Assistant.
