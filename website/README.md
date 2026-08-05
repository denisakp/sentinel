# Sentinel documentation site

The source of the Sentinel documentation site, built with [Docusaurus](https://docusaurus.io/).

```bash
npm ci
npm start          # dev server at http://localhost:3000
npm run build      # production build; this is the gate, see below
npm run serve      # preview the production build
npm run a11y       # accessibility audit (needs `npm run serve` running)
```

**Writing a page? Read [`CONTRIBUTING-DOCS.md`](./CONTRIBUTING-DOCS.md) first.** It has the authoring
convention, the page template for each section, and the rules that keep ~88 pages written by
different people reading as one document.

## Things that will bite you

**`npm run build` is the test suite.** Broken internal links and stale heading anchors are build
failures, not warnings. If it builds, the links resolve.

**Redirects and the search index only exist in production builds.** A redirect that appears broken
under `npm start` is not a bug; check it against `npm run build`.

**Do not add `static/CNAME` or set a custom domain in the repository's Pages settings** until DNS for
`sentinel.denisakp.me` actually resolves. Configuring a custom domain makes GitHub redirect the
`github.io` URL to it; with DNS unresolved, the site becomes unreachable at *both* addresses.

**The Pages publishing source must stay "GitHub Actions".** Never select "deploy from a branch
`/docs` folder"; that would try to publish the repository's raw `docs/runbooks/` Markdown as the
website.

## Hosting targets

`baseUrl` is baked into every link at build time, and the two hosting targets serve from different
paths. `DEPLOY_TARGET` selects between them:

| `DEPLOY_TARGET` | Serves from | When |
|---|---|---|
| `ghpages` (default) | `https://denisakp.github.io/sentinel/` | Until DNS resolves |
| `custom` | `https://sentinel.denisakp.me/docs/` | After DNS resolves and CNAME ships |

## Two directories named `docs`

- `website/docs/`: this site's content.
- `docs/runbooks/` at the repository root: the preserved operator runbooks. Not part of this build.
