# Empty-state illustrations

Page-level empty states may use a branded illustration. Compact empty states in
sidebars, lists, menus, search results, and settings should continue to use text
or a small functional icon.

## Visual standard

- Use `EmptyStateIllustration` as the SVG frame.
- Use the `144 × 112` view box and default `144 × 112px` rendered size.
- Use `emptyStateIllustrationStrokeWidth` for every visible line.
- Use round line caps and joins on outlined shapes.
- Use semantic neutral colors for the main artwork.
- Use at most one quiet brand accent; it must also work in dark mode.
- Every shape must communicate the empty state. Do not add disconnected lines,
  dots, or other decorative residue.
- Keep illustrations decorative with `aria-hidden` and `focusable="false"`;
  the nearby heading and description provide the accessible meaning.

Add each page-level variant to this directory as
`<Feature>EmptyIllustration.tsx`.
