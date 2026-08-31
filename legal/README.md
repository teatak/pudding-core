# Third-party notices

Pudding release bundles include software maintained by third parties. Their license texts and attribution
notices are generated from the exact locked dependencies used for a release:

```bash
npm run legal:generate
```

The command writes `dist/legal/`. The packaging pipeline copies that directory to
`Pudding.app/Contents/Resources/legal`, and the release verifier rejects bundles with missing notices.

Do not edit generated notice files by hand. Update the relevant dependency, lockfile, or source license instead.
