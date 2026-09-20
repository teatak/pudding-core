# Third-party notices

The root `LICENSE` covers Pudding Core. Native dependency license texts are kept in
`legal/native/`; Go modules and language servers retain their respective upstream licenses.

The desktop repository generates distribution notices from the exact locked core, npm,
Electron and native dependencies. Run `npm run legal:generate` in `pudding-desktop`.
Core builds do not require the desktop notice generator or its private sources.

Do not edit upstream license texts or generated notices by hand.
