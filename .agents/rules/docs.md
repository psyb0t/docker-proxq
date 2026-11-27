# Documentation

Prefix: DOC. The pages are `docs/`, built into a site by Docusaurus from `website/`.

- **DOC1:** `README.md` is the front door: what proxq is, a quick start, and links to the site with full `https://psyb0t.github.io/docker-proxq/...` URLs. Reference material goes in `docs/`.
- **DOC3:** Build with `make docs` before handing off. The build fails on a broken link or heading anchor. A new page also goes into `website/sidebars.mjs` and `website/static/llms.txt`.
- **DOC4:** Defaults, limits and error messages in the docs are copied from `internal/config` and `internal/proxy`.
