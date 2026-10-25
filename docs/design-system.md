# Workbench Design System

## Purpose

Workbench is the visual system for the HA Lightcraft web editor: a quiet lighting
workshop where people compose colors and sequences, assign them to schedules,
inspect their timing, and deliberately publish to Home Assistant. Surfaces are
dark and neutral, one mint accent marks actions, selection, and focus, and the
signal colors have fixed meanings: amber means unpublished, pink-red means
destructive or failed. Lighting colors belong only in swatches and previews; the
interface around them stays visually stable so the lights are what stand out.

There is one theme, dark. The page sets `color-scheme: dark` so native controls
and scrollbars match.

## Where values live

- [`web.css`](../web.css) defines every token as a CSS custom property in the
  `:root` rule at the top of the file; `web.go` embeds it and inlines it into
  `web.html`'s `<style>` block. The sequence preview styles live in the same file and use
  the same tokens.
- `web.html`'s `<head>` loads IBM Plex Sans and Plex Mono from Bunny Fonts.
- [`theme_contrast_test.go`](../theme_contrast_test.go) reads those tokens and
  checks the [contrast matrix](#contrast).

Token names are stable. Change a value in `:root`; do not rename a token to suit
one screen. Rules never contain a hex color. The only colors outside `:root` are
lighting colors rendered from data into `style` attributes.

This document never repeats hex values. Read `:root` when you need one.

## Color roles

| Token | Use for |
|---|---|
| `--bg` | Page background behind all content. |
| `--panel` | Workspace panels and the editor. |
| `--inset` | Wells inside the editor: the sequence preview and its empty state. |
| `--field` | Inputs, selects, and textareas. |
| `--code` | YAML and diff blocks. |
| `--raised` | Secondary buttons and button-style links. |
| `--raised-strong` | Compact step operation buttons (move, remove). |
| `--hover` | Hover fill for catalog entries and light-picker rows. |
| `--line` | Separators and panel edges. Decorative only. |
| `--control-line` | Edges of inputs, selects, the light picker, and secondary buttons, where the border is how you find the control. |
| `--text` | Primary text. |
| `--muted` | Labels, descriptions, metadata, and status lines. |
| `--accent` | Primary button fill, active navigation text, the focus ring, and the Group badge. |
| `--on-accent` | Text on `--accent` fills. |
| `--accent-soft`, `--accent-soft-line` | Fill and border of the active navigation item. |
| `--selected`, `--selected-text` | Fill and text of the selected catalog entry. |
| `--notice`, `--notice-line` | Success notice fill and border. |
| `--error`, `--error-line` | Error notice fill and border. |
| `--danger` | Destructive action text and the import disclosure label. |
| `--danger-line` | Border of outlined destructive buttons. |
| `--pending` | The "Unpublished changes" indicator. |

Use flat surfaces and 1px borders. Do not add shadows, gradients, or another
accent color.

## Units

Sizes are written in rem, so text, controls, and spacing grow with the text size a
person sets in their browser. Page zoom scales everything either way.

- **rem** for font sizes, spacing, control heights, widths, radii, and grid
  tracks. This is the default.
- **em** for values that follow the text they sit in, such as the padding inside
  the Group badge.
- **px** only for hairlines that stay fixed at any text size: border widths, the
  3px selected-entry edge, focus-ring width and offset, and the 999px pill radius.
- The 760px breakpoint stays in px.
- `html` and `body` never set a pixel font size. The root stays at the browser
  default; the page text is 0.9375rem.

This document gives a value as rem with its pixel equivalent at the default 16px
in brackets, such as 2.75rem (44px). The rem is the rule; the pixel figure is for
reading.

## Accessibility

The baseline is **WCAG 2.2 level AA**. On top of that, the editor adopts the AAA
target for text contrast (WCAG 1.4.6) and a 44px comfort size for controls.

### Contrast

Contrast uses the WCAG relative-luminance formula. `TestThemeContrast` checks
this matrix and fails if a value slips:

| Foreground | Backgrounds | Target |
|---|---|---|
| `--text`, `--muted`, `--accent`, `--danger`, `--pending`, `--selected-text` | `--bg`, `--panel`, `--inset`, `--field`, `--code` | 7:1 (AAA, WCAG 1.4.6) |
| `--text`, `--muted` | `--raised`, `--hover` | 7:1 |
| `--text` | `--raised-strong`, `--notice`, `--error` | 7:1 |
| `--selected-text`, `--muted` | `--selected` | 7:1 |
| `--accent` | `--accent-soft` | 7:1 |
| `--on-accent` | `--accent` | 7:1 |
| `--danger` | `--error` | 7:1 |
| `--control-line`, `--danger-line`, `--accent` (focus ring) | `--bg`, `--panel`, `--inset`, `--field`, `--code` | 3:1 (AA, WCAG 1.4.11) |

The matrix lists the pairs the page actually uses, not every combination: for
example, only `--text` sits on `--raised-strong`.

When you add a token, a pair, or an interaction state that is not in this
matrix, add it to the test before you merge. Do not show a state by changing the
opacity of a fill that carries text, because the result is not in the matrix;
give the state its own token. Disabled controls are the only exception.

### Decisions and their reasons

- **Text meets 7:1.** The editor is a dense dark workbench used in dim rooms,
  often while watching the lights themselves, so text aims above the AA minimum.
  This applies to headings, labels, and metadata too. `--muted` was lightened to
  reach 7:1 on the hover and selected fills, where catalog metadata sits.
- **Control edges meet 3:1** (WCAG 1.4.11). Fields sit on `--panel` with a
  `--field` fill that is barely different, so the border is the only edge that
  shows where a field is. `--control-line` outlines fields, the light picker, and
  secondary buttons. Separators and panel edges keep the fainter `--line`,
  because they don't identify a control.
- **Selection never relies on fill alone.** The selected catalog entry adds a 3px
  `--accent` left edge and `aria-current`; active navigation adds an
  `--accent-soft-line` border, `--accent` text, and `aria-current="page"`.
- **Controls are at least 2.75rem (44px) tall.** Fields, selects, buttons,
  navigation links, and button-style links share one `min-height`. That is above
  WCAG 2.5.8's 24px minimum and meets 2.5.5's 44px target (AAA).
- **Disabled controls are exempt** (WCAG 1.4.3). They use `--disabled` opacity,
  and nearby text says why the control is disabled and what to do next.
- **Status always has words.** "• Unpublished changes", the Group badge, and
  every notice carry text; their color only reinforces it.

### Interaction baseline

- **Focus.** Links, buttons, inputs, selects, and summaries show a 2px `--accent`
  outline with a 3px offset on `:focus-visible` (WCAG 2.4.7). Do not remove an
  outline without replacing it.
- **Keyboard.** Every operation works from the keyboard, including step move and
  remove. After a move, focus follows the moved step; after removal it goes to
  the neighbouring step's color.
- **Names.** Actions are buttons; navigation is links. Step buttons have names
  such as "Move step 2 up", renumbered after every change. The header icon is
  decorative (`alt=""`) because the product name sits next to it.
- **Labels.** Every field has a visible label above it, including units and
  accepted formats. There are no placeholder-only fields.
- **Errors.** The page notice uses `role="alert"` and names the failed operation.
  When the error belongs to a field, the field also gets `aria-invalid` and
  `aria-describedby` pointing at a `.field-error` message beneath it, and focus
  moves to it on load (WCAG 3.3.1). The message says how to fix the value. A
  rejected edit re-renders with what the person submitted; never clear their
  input.
- **Status.** Success messages use `role="status"`.
- **Motion.** The sequence preview is the only animation. Under
  `prefers-reduced-motion` it starts paused and its transitions are off; when it
  has more than one step it always offers Pause/Resume.
- **Checks before merging visual work.** Look at the change at desktop width and
  at 320px, at 200% text size, and with keyboard only, with long names and IDs.
  YAML blocks and the light picker may scroll inside themselves; the page must
  not scroll sideways (WCAG 1.4.10).

### Known gaps

None open. When a rule above is not met somewhere, list it here with the area
that owns the fix; new work meets the rules from the start.

## State

Never signal a state with color alone.

| State | Presentation |
|---|---|
| Unpublished | "• Unpublished changes" in `--pending` beside "Local draft editor". |
| Active navigation | `--accent-soft` fill, `--accent-soft-line` border, `--accent` text, `aria-current="page"`. |
| Selected catalog entry | `--selected` fill, `--selected-text`, 3px `--accent` left edge, `aria-current="true"`. |
| Group target | `Group` text badge, `--accent` fill, pill radius. Groups list first. |
| Success | `.notice` with `--notice` colors and `role="status"`. |
| Error | `.notice.error` with `--error` colors and `role="alert"`, naming the operation: "Could not update draft", "Publish failed", "Import failed", "Inventory refresh failed", "Playback control failed". |
| Disabled | `--disabled` opacity, `not-allowed` cursor, and a nearby reason. |
| Offline reference | Option text ends "offline draft reference" or "current draft value"; never presented as verified inventory. |
| Empty | Names what is missing and points at the next action: create a color, add a sequence, refresh Inventory. An empty catalog and unavailable inventory read differently. |

## Typography

| Role | Token | Use for |
|---|---|---|
| Interface | `--font-sans` (IBM Plex Sans, 400, 600, 700) | All UI text. The page default. |
| Technical | `--font-mono` (IBM Plex Mono, 400) | Entity and object IDs (`code.id`), YAML, and diffs. |

Plex fits a workbench: engineered letterforms, and numerals that stay aligned.
The page sets `tabular-nums`, so CIE coordinates, times, and brightness values
line up in lists and agenda rows. IDs are set in Plex Mono so `light.porch` and
"Porch" never read as the same thing; wrap an ID in `<code class="id">`, which
keeps the surrounding size and color.

Fonts load from Bunny Fonts in `web.html`'s `<head>`. Both tokens keep a real
fallback stack (`system-ui`, `ui-monospace`), because the editor may run without
internet access; the layout must still work with the fallbacks. Do not add a
weight or family without changing this table first.

Visual size and heading level are separate: use the heading element the page
structure needs.

### Type scale

Use only these sizes.

| Size | Line height | Use for |
|---|---|---|
| 1.375rem (22px) | inherited | Product name (`h1`). |
| 1.125rem (18px) | inherited | Section and editor headings (`h2`). |
| 0.9375rem (15px) | 1.45 | Body text, controls, and subsection headings (`h3`). The page default. |
| 0.875rem (14px) | inherited | The sequence preview heading. |
| 0.8125rem (13px) | inherited | Labels, status lines (`.status`), and field errors. |
| 0.75rem (12px) | inherited; 1.5 for code | Metadata (`.meta`), badges, preview details, and code. |

Small sizes are for secondary information, never for the main task. Bold (700)
is for primary buttons, badges, and `strong`; summaries use 600.

## Radius

| Token | Value | Use for |
|---|---|---|
| `--radius-sm` | 0.3125rem (5px) | Color swatches, the color picker, and preview bars. |
| `--radius` | 0.5rem (8px) | Fields, buttons, navigation, catalog entries, notices, code blocks, the light picker, and the preview well. |
| `--radius-lg` | 0.75rem (12px) | Panels and the editor. |
| (literal) | 999px | Pill badges. |

## Layout

- The header holds the 2.5rem (40px) icon, the product name, the "Local draft editor"
  status, and navigation. Its content aligns with `main`.
- `main` is centered with a 68.75rem (1100px) maximum width and 1.5rem top,
  1.25rem side, and 3.75rem bottom padding.
- Navigation order is **Colors, Sequences, Schedules, Agenda, Home Assistant**, a
  divider, then the secondary **Inventory**. Use the same labels in headings and
  help text.
- Workspaces with a catalog use `.grid`: a 14.375rem (230px) catalog and a flexible
  editor, 1.75rem apart. The catalog has no fill or border; "New …" sits above its entries.
  The editor is a panel with 1.625rem padding.
- Spacing rhythm: 0.5rem within a group, 0.75–1rem between related fields,
  1.25–1.5rem between workspace regions. Match nearby components rather than adding a value.
- Show the human name first and the stable ID as `.meta` beneath it, so similar
  devices stay distinguishable. Long names and IDs wrap.

| Workspace | Structure |
|---|---|
| Colors | Catalog; editor with picker and numeric CIE coordinates. |
| Sequences | Catalog; editor with the saved preview above the steps. |
| Schedules | Catalog; editor grouped as Program, Light targets, Timing, Finish behavior; Live playback below the form. |
| Agenda | One panel, date navigation, two weekly tables; schedule names link to editing. |
| Home Assistant | Readiness and publish first; changes and YAML disclosures; import last. |
| Inventory | Read-only light list with names, IDs, state, location, and capabilities; refresh action. |

At 760px and below, the header and catalog/editor stack, navigation wraps,
catalog entries become a horizontally scrolling row, agenda weeks become one
column, step fields reflow to two columns, schedule date/time pairs stack, and
editor padding drops to 1.125rem. Visual order always matches DOM order.

## Inputs

- Native controls only: `input`, `select`, `textarea`, `details`. No custom
  pickers or dependencies.
- Fields are full width with a `--field` fill, `--control-line` border, and
  0.5625rem 0.625rem padding, at least 2.75rem (44px) tall. Textareas start at
  4.75rem and resize vertically.
- Labels sit above their field in `--muted` 0.8125rem, with 0.75rem above and
  0.3125rem below.
  Include units and formats: "Brightness (%)", "Hold (seconds)",
  "Start time (HH:MM or sunset)", "Start date (M-D or YYYY-M-D)".
- Use selects for catalog and inventory choices; never ask for free-text entity
  IDs when inventory has the choices.
- Light targets are checkboxes in the scrollable light picker (16.25rem max height),
  a labelled group.
- Changing a parent choice, such as the program type or WLED light, hides
  irrelevant fields, updates `required`, and clears dependent choices.
- A field error appears directly beneath its control in `--danger`, and the
  control carries `aria-invalid`. Server code ties an error to a field with
  `fieldErrorf`; errors that are not about one field stay in the page notice.
- "All day" disables the time fields but keeps the date range. Dates are
  inclusive; M-D is annual and YYYY-M-D is one-off.

## Buttons and borders

Borders mark what you can type into or choose from. Use links for navigation and
buttons for changes. Pick the first row that fits:

| Role | Class | Use for |
|---|---|---|
| Main action | `button` (default) | The one save or publish action in a form: "Update draft", "Publish changes", "Refresh inventory". At most one per form. |
| Destructive | `.danger` | Delete, import-replace. Text names verb and object, such as "Delete color". |
| Secondary | `.secondary` | Add step, Pause/Resume preview, and other non-saving actions. |
| Step operation | `.step-actions button` | Move and remove inside a step row. |
| Navigation as button | `a.button` | "New …" and agenda date navigation. |

Rules:

- "Update draft" saves locally. "Publish" is only for sending to Home Assistant.
  Never blur the two.
- Do not add an `--accent` fill to anything that is not the main action.
- Prevent duplicate live operations while a request is pending, and report the
  real outcome in text.
- Disabled buttons stay visible when they explain readiness, with the reason
  beside them.

## Sequence preview

- The preview shows saved sequence timing, not physical light output or WLED
  effects. Say so on the preview.
- It sits in the sequence editor after the name and loop setting, in an `--inset`
  well, with a compact empty state when nothing is selected.
- It pairs a color bar with the step name and index, CIE/RGB values, and
  brightness in text. A palette strip summarizes the steps; per-light detail sits
  under a "Target lights" disclosure.
- Brightness is shown as text and opacity together, never opacity alone.

## Disclosure

Use native `details`/`summary`. The changes disclosure opens when a diff exists;
full generated YAML stays collapsed. Import controls sit in a collapsed
disclosure with a `--danger` label. Code blocks scroll internally (35rem max
height) and stay selectable.

## Safety scopes

Every action shows its scope before it is submitted. Do not weaken the server's
token, stale-draft, validation, backup, verification, or rollback protections to
simplify a screen; those decisions live in the root [`DESIGN.md`](../DESIGN.md).

| Scope | Presentation |
|---|---|
| Local edit | Draft wording and the visible unpublished state. |
| Delete | Explicit object and confirmation; schedules are disabled rather than deleted. |
| Publish | Change summary, readiness reason, visible diff, typed `PUBLISH`. Success is reported only after verification. |
| Import | Explains that baseline and draft are replaced and local colors kept; typed `IMPORT` when replacing a proposed package. |
| Live playback | Separate from the draft form; typed `PAUSE` or `RESUME`. "Enabled" is draft configuration, not playback. |

WLED owns its effects; Lightcraft only selects an existing preset or playlist.
