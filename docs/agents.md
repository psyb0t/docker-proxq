---
title: "Agent integrations"
description: "Install the proxq skill in Claude Code, Codex or OpenClaw so an agent can submit requests to a running proxq, poll jobs and read results."
nav_order: 8
---

# Agent integrations

The [proxq skill](https://github.com/psyb0t/docker-proxq/tree/main/.agents/skills/proxq) teaches an AI agent to use a proxq instance that is already running: submit a request, poll the job, fetch the content and cancel a job it submitted. It does not deploy or reconfigure proxq.

The skill works in any agent that reads `.agents/skills/` (such as [peen](https://github.com/psyb0t/peen)), and installs natively in the clients below. It reads the instance address from `PROXQ_URL`.

## Claude Code

```bash
claude plugin marketplace add psyb0t/agents
claude plugin install proxq@psyb0t
```

Claude Code asks for the proxq base URL (`PROXQ_URL`) when the plugin is enabled.

## Codex

```bash
codex plugin marketplace add psyb0t/agents
codex plugin add proxq@psyb0t
```

Installed through the marketplace, the skill is invoked as `$proxq:proxq`. Codex also picks the skill up with no install in any repository that contains `.agents/skills/`, where it is invoked as plain `$proxq`.

## OpenClaw

The skill is published to ClawHub on every release:

```bash
openclaw skills install @psyb0t/proxq
```

## What the agent is told

The skill carries the same warnings as the [security](security.md) page: proxq has no authentication, it makes outbound requests on the caller's behalf, and there is no ownership check on job IDs, so the agent cancels only jobs it submitted or that you named.
