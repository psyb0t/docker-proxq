# Agent surface

Prefix: AGT. The skill and the plugin manifests under `.agents/` describe proxq to other agents. They are public and published to ClawHub on every tag.

- **AGT1:** The skill covers using an already-running proxq, links the docs for setup and does not document the installer, the Docker images or the config tables. A behavior, route or header change updates `.agents/skills/proxq/SKILL.md` in the same change.
- **AGT2:** The Codex manifest `version` in `.agents/.codex-plugin/plugin.json` moves with the release tag, in the release commit. The Claude manifest in `.agents/.claude-plugin/plugin.json` carries no version.
- **AGT3:** The skill keeps its `Security & safety` section: no built-in auth, the SSRF surface, and the cancel guardrail.
- **AGT4:** `publish-to-clawhub` in `.github/workflows/pipeline.yml` names the skill directory in `skill_names`. Renaming or adding a skill directory updates that list.
