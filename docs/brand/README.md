# N2API Brand Assets

<p align="center">
  <img src="../../frontend/static/n2api-logo.svg" alt="N2API logo" width="160" height="160" />
</p>

The N2API mark combines a bold `N` gateway silhouette with a teal routing node that splits into two outbound paths. The final mark is an original project asset and does not incorporate OpenAI, ChatGPT, or other third-party brand marks.

## Production Assets

| Asset | Purpose |
| --- | --- |
| [`frontend/static/n2api-logo.svg`](../../frontend/static/n2api-logo.svg) | Canonical vector source and README logo |
| [`frontend/static/favicon.svg`](../../frontend/static/favicon.svg) | Website favicon variant |
| [`exports/n2api-logo-2048.png`](exports/n2api-logo-2048.png) | 2048 x 2048 raster export for systems that cannot use SVG |

The SVG is the source of truth. Regenerate raster exports from it instead of editing the PNG.

## Generation Record

- Date: 2026-07-13
- Workflow: `imagegen` fallback CLI using the Image API
- Model: `gpt-image-2`
- Quality: `low`
- Requested size: `1024x1024`
- Returned size: `1254x1254` (the compatible provider remapped the requested dimensions)
- Output format: PNG
- Variants per prompt: 1

API credentials and provider authentication details are intentionally not stored in this repository. Reusing the same prompts and parameters is not expected to reproduce identical pixels.

## Exact Prompts

### Concept A

```text
Use case: logo-brand
Primary request: Create an original abstract symbol for N2API, a personal AI API and account gateway. Combine an angular N-like data path with one small central routing node and two balanced outbound routes.
Style/medium: vector logo mark, flat colors, minimal, precise geometry
Composition/framing: single centered mark, clear silhouette, balanced negative space, generous margin
Constraints: white background; use only near-black #0d0d0d and teal #10a37f; no text; no gradients; no mockup; no 3D; no watermark; no OpenAI, ChatGPT, or other trademarked marks
```

### Concept B

```text
Use case: logo-brand
Primary request: Design an original N2API favicon mark for a personal AI API gateway. Build a bold geometric letter N from one incoming route, a central square routing hub, and exactly two outgoing route terminals. The symbol must remain recognizable at 16 pixels.
Style/medium: strict flat vector logo, heavy geometric strokes, minimal, technical but calm
Composition/framing: single centered square-friendly mark, compact silhouette, generous outer padding, no wordmark
Constraints: pure white background; only near-black #0d0d0d and teal #10a37f; no thin lines; no gradients; no shadows; no texture; no mockup; no 3D; no watermark; no OpenAI or ChatGPT marks
```

### Concept C

```text
Use case: logo-brand
Primary request: Design an original N2API app icon for a personal AI routing gateway. Use two bold opposing path brackets that form an N in negative space around one teal routing node, with a subtle second-route idea expressed through the geometry rather than text.
Style/medium: strict flat vector logo, minimal monogram, strong silhouette, balanced negative space
Composition/framing: single centered square-friendly icon, symmetrical visual weight, generous outer padding, no wordmark
Constraints: pure white background; only near-black #0d0d0d and teal #10a37f; no thin lines; no gradients; no shadows; no texture; no mockup; no 3D; no watermark; no OpenAI or ChatGPT marks
```

## Original Model Outputs

These PNGs are preserved byte-for-byte from the model response.

| Concept | Original | Dimensions | Bytes | SHA-256 |
| --- | --- | --- | ---: | --- |
| A | [`n2api-logo-concept-a-original.png`](source/n2api-logo-concept-a-original.png) | 1254 x 1254 | 740265 | `61d62b6e04f8884ab427138eb07ebf8652064ddf3e9bf8b0a0cb936d42cdb51d` |
| B | [`n2api-logo-concept-b-original.png`](source/n2api-logo-concept-b-original.png) | 1254 x 1254 | 860640 | `b4a74f004105b4307a6d308ccc4808382287f2db022c222b308ee37522bd1740` |
| C | [`n2api-logo-concept-c-original.png`](source/n2api-logo-concept-c-original.png) | 1254 x 1254 | 830036 | `ee8df26106c458f4230922f9a1d32586001a1ac19c3a3abe2d470258a235b34c` |

The 2048px production export has SHA-256 `c673a1f197017dc437b81ec33a32db3b906a6bb3c70cf361937df5fb02fa2da8`.

## Finalization Notes

The final SVG is a deterministic redraw rather than a direct trace of one model output. It combines:

- Concept A's central routing split.
- Concept B's explicit gateway-to-two-exits meaning.
- Concept C's compact, favicon-friendly silhouette.
- The normative N2API palette from `DESIGN.md`: ink `#0d0d0d`, accent `#10a37f`, and white canvas.

The mark was visually checked at 16px, 64px, 256px, and 2048px. Keep the two-color geometry, generous outer margin, and strong silhouette when producing future variants.
