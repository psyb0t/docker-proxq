# Documentation

Prefix: DOC. The site is `docs/`, built by GitHub Pages with Jekyll and the just-the-docs theme.

- **DOC1:** `README.md` is the front door: what proxq is, a quick start, and links to the site with full `https://psyb0t.github.io/docker-proxq/...` URLs. Reference material goes in `docs/`.
- **DOC3:** Build with `make docs`, then run the link checker over `.testing/docs-site` before handing off. Wrap any `{{` or `{%` in a code block with `{% raw %}` tags inside HTML comments.
- **DOC4:** Defaults, limits and error messages in the docs are copied from `internal/config` and `internal/proxy`.
