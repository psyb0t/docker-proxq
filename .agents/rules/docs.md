# Documentation

Prefix: DOC. The site is `docs/`, built by GitHub Pages with Jekyll and the just-the-docs theme.

- **DOC1:** Each fact has one home. `README.md` is the front door: what proxq is, a quick start, and links to the site with full `https://psyb0t.github.io/docker-proxq/...` URLs. Reference material goes in `docs/`.
- **DOC2:** Every page has front matter with a unique `title` and a `description` of 50 to 170 characters. Page-to-page links use the relative `.md` path. A link to a repository file outside `docs/` uses the full `https://github.com/psyb0t/docker-proxq/blob/main/<path>` URL.
- **DOC3:** Build with `make docs`, then run the link checker over `.testing/docs-site` before handing off. Wrap any `{{` or `{%` in a code block with `{% raw %}` tags inside HTML comments.
- **DOC4:** Write from the code, not from older docs. Defaults, limits and error messages are copied from `internal/config` and `internal/proxy`.
- **DOC5:** No em dashes, "directory" not "folder", one paragraph per source line.
