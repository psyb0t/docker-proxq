# Agent surface

Prefix: AGT. The skill and the plugin manifests under `.agents/` describe proxq to other agents. They are public and published to ClawHub on every tag.

- **AGT1:** A behavior, route, header or config change updates `.agents/skills/proxq/SKILL.md` and `.agents/skills/proxq/references/setup.md` in the same change. The config tables in `setup.md` mirror `internal/config/config.go`.
- **AGT2:** The Codex manifest `version` in `.agents/.codex-plugin/plugin.json` moves with the release tag, in the release commit. The Claude manifest in `.agents/.claude-plugin/plugin.json` carries no version.
- **AGT3:** The skill keeps its `Security & safety` section: no built-in auth, the SSRF surface, and the cancel guardrail.
- **AGT4:** `publish-to-clawhub` in `.github/workflows/pipeline.yml` names the skill directory in `skill_names`. Renaming or adding a skill directory updates that list.
