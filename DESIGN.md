---
version: alpha
name: N2API OpenAI-Inspired Operational Design
description: A restrained OpenAI-inspired design system for the N2API personal AI gateway admin UI.
colors:
  canvas: "#ffffff"
  surface: "#fafafa"
  panel: "#ffffff"
  panel-muted: "#f5f5f5"
  ink: "#0d0d0d"
  ink-soft: "#1f2933"
  text: "#3c3c3c"
  text-muted: "#6e6e6e"
  text-faint: "#9b9b9b"
  border: "#e5e5e5"
  border-soft: "#ededed"
  accent: "#10a37f"
  accent-hover: "#0a7a5e"
  accent-soft: "#e8f5f0"
  danger: "#ef4146"
  warning: "#f5a623"
  info: "#2563eb"
typography:
  h1:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, sans-serif"
    fontSize: "32px"
    fontWeight: 600
    lineHeight: 1.15
    letterSpacing: "0"
  h2:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, sans-serif"
    fontSize: "24px"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0"
  body:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "0"
  label:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, sans-serif"
    fontSize: "13px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "0"
  mono:
    fontFamily: "ui-monospace, JetBrains Mono, Menlo, Consolas, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "0"
rounded:
  sm: "6px"
  md: "8px"
  lg: "12px"
  pill: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  "2xl": "32px"
  "3xl": "48px"
  "4xl": "64px"
components:
  button-primary:
    background: "{colors.ink}"
    color: "{colors.canvas}"
    radius: "{rounded.md}"
  button-secondary:
    background: "{colors.panel}"
    color: "{colors.ink}"
    border: "{colors.border}"
    radius: "{rounded.md}"
  input:
    background: "{colors.panel}"
    border: "{colors.border}"
    radius: "{rounded.md}"
  card:
    background: "{colors.panel}"
    border: "{colors.border-soft}"
    radius: "{rounded.md}"
---

# N2API Design System

## Overview

N2API should feel like a quiet operational tool for a technical owner: calm, precise, readable, and trustworthy. The interface borrows from OpenAI's public visual language only at the level of restraint: near-monochrome surfaces, generous white space, precise type, minimal decoration, and a single teal accent for active or successful states.

This is not an OpenAI-branded product. Do not use OpenAI logos, wordmarks, Blossom marks, ChatGPT marks, GPT marks, or model names as product branding. OpenAI-related names may appear only where they accurately describe an integration, provider, or model setting.

Design density should be moderate. N2API is an admin dashboard, not a marketing site, so prefer compact tables, settings groups, status rows, and focused forms over large hero sections or decorative storytelling.

## Colors

Use a mostly neutral palette:

- `canvas` (`#ffffff`) for the app background when the screen needs maximum clarity.
- `surface` (`#fafafa`) for subtle page bands and background contrast.
- `panel` (`#ffffff`) for form groups, tables, and repeated cards.
- `ink` (`#0d0d0d`) for primary text and primary actions.
- `text` (`#3c3c3c`) for normal reading text.
- `text-muted` (`#6e6e6e`) and `text-faint` (`#9b9b9b`) for metadata, helper copy, timestamps, and placeholders.
- `border` (`#e5e5e5`) and `border-soft` (`#ededed`) for hairline separation.
- `accent` (`#10a37f`) only for selected state, healthy status, links, and positive progress.
- `danger`, `warning`, and `info` only for semantic states.

Do not use gradient backgrounds, decorative color blobs, heavy blue/purple palettes, or saturated multi-color dashboards. The UI should let data, configuration, and request state carry the visual weight.

## Typography

Use Inter or the system sans stack for all UI. If Inter is unavailable, system fonts are acceptable. Do not depend on proprietary OpenAI Sans.

Type hierarchy:

- Page title: 32px, 600, line-height 1.15.
- Section title: 24px, 600, line-height 1.2.
- Panel title: 18px, 600, line-height 1.3.
- Body: 16px, 400, line-height 1.6.
- Dense UI text: 14px, 400 or 500, line-height 1.5.
- Labels and metadata: 13px, 500, line-height 1.4.
- Code, IDs, tokens, and request samples: 13px monospace, line-height 1.55.

Letter spacing should stay at `0`. Do not use negative letter spacing in this app. Avoid font weights above 600 unless a browser default makes it unavoidable.

## Layout

Use full-width application sections with constrained inner content. Keep page max width around 1120-1200px. Use 24px gutters on desktop and 16px gutters on mobile.

Preferred structure:

- Top-level app shell with simple navigation.
- Page header with title, short status metadata, and primary action.
- Main content split into settings panels, status summaries, tables, and detail drawers.
- Forms grouped by operational task: provider login, API keys, model routing, logs, health.

Spacing scale:

- 4px for icon/text micro gaps.
- 8px for compact groups.
- 12px for form element spacing.
- 16px for standard internal padding.
- 24px for panel padding and grid gaps.
- 32px and 48px for page-level rhythm.

Avoid nested cards. Use cards for repeated items, settings panels, and modal/drawer surfaces only.

## Elevation & Depth

Default depth is flat. Use borders and spacing before shadows.

- Panels: `1px solid #ededed`, no shadow.
- Hoverable rows/cards: optional `0 4px 16px rgba(13, 13, 13, 0.06)`.
- Modals/drawers: subtle shadow plus scrim, never heavy glass effects.
- Sticky nav or toolbar: white background, hairline bottom border.

Do not use neumorphism, glassmorphism, glow effects, or heavy drop shadows.

## Shapes

Use restrained radii:

- Buttons, inputs, select boxes, and panels: 8px.
- Larger dialogs/drawers: 12px.
- Chips, badges, and token pills: full pill radius.
- Tables and dense lists: 6px to 8px.

Avoid overly round cards. A technical operations app should feel precise, not playful.

## Components

Buttons:

- Primary: black background, white text, 8px radius, 10px 16px padding.
- Secondary: white background, black text, hairline border.
- Accent: teal background only for provider login, healthy status actions, or success path.
- Destructive: text or outline first; filled red only for confirmed destructive actions.

Inputs:

- White background, hairline border, 8px radius.
- Focus uses teal border plus a soft teal ring.
- Helper text should be concise and below the field.
- Secrets and tokens should have reveal/copy controls, never display by default.

Tables and logs:

- Use compact rows with clear timestamp, provider, route, status, latency, and action columns.
- Prefer tabular numbers for latency, counts, token usage, and status codes.
- Use color sparingly: semantic status dot or badge, not full colored rows.

Badges:

- Neutral badges use gray text on `panel-muted`.
- Success badges use teal text on `accent-soft`.
- Warning/error badges use semantic colors with soft backgrounds.

Navigation:

- Keep nav labels short.
- Use icons only when they clarify repeated actions.
- Avoid large logo treatment. N2API is the product brand; OpenAI is only an integration label.

## Do's and Don'ts

Do:

- Build quiet, fast, operational screens.
- Use whitespace, alignment, and typography as the primary design tools.
- Make streaming status, OAuth state, token freshness, and provider health easy to scan.
- Keep settings forms predictable and reversible where possible.
- Use real UI states: loading, empty, disabled, error, success, stale token, disconnected provider.
- Keep color roles semantic and consistent.

Don't:

- Do not use OpenAI marks as N2API branding.
- Do not imply OpenAI endorsement, partnership, sponsorship, or official status.
- Do not put model names such as GPT in the product name, app title, or logo.
- Do not use gradients, bokeh, decorative orbs, oversized hero sections, or marketing-page composition.
- Do not make dashboard cards huge when a dense table or settings group is more useful.
- Do not use dark mode as the default V1 design unless explicitly requested later.
- Do not expose full OAuth tokens, refresh tokens, or API keys in the UI.

## Responsive Behavior

Desktop:

- Use two-column layouts for settings and status summaries.
- Tables may remain full width with horizontal overflow only for log-heavy views.
- Keep primary actions in the page header or panel header.

Tablet:

- Collapse secondary side panels below primary content.
- Keep forms one column if field labels become cramped.

Mobile:

- Use single-column layouts.
- Replace dense tables with stacked log rows.
- Keep destructive actions behind confirmation dialogs.
- Ensure every button label fits without viewport-scaled font sizing.

## Accessibility

- Maintain at least WCAG AA contrast for text and controls.
- Every interactive element needs a visible focus state.
- Do not rely on color alone for status; pair color with labels or icons.
- Inputs need explicit labels, not placeholder-only labeling.
- Use semantic HTML for buttons, forms, tables, and navigation.
- Keep motion subtle and avoid required animation for comprehension.

## Agent Prompt Guide

When building N2API UI:

- Follow this `DESIGN.md` as the source of truth.
- Use SvelteKit and Tailwind CSS utilities, but keep class composition readable.
- Build operational dashboard screens, not landing pages.
- Prefer flat white panels, hairline borders, restrained spacing, and teal accents.
- Use compact, scannable layouts for provider state, API keys, model routes, logs, and health.
- Never use OpenAI brand assets or imply official affiliation.

## Sources

This file is adapted for N2API from:

- OpenAI Brand Guidelines: https://openai.com/brand/
- Open Design's OpenAI-inspired DESIGN.md reference: https://github.com/nexu-io/open-design/blob/main/design-systems/openai/DESIGN.md
- Google Labs DESIGN.md format documentation: https://github.com/google-labs-code/design.md/blob/main/docs/spec.md
