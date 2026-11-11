# TraceForge session workflow

- The user provides each session's scope. Implement that session without silently implementing future sessions or rewriting unrelated code.
- Follow the approved direction: a Go/OpenTelemetry/ClickHouse observability core, PostgreSQL metadata, a Python AgentOps/RAG evaluation runner, and a Next.js/TypeScript interface. These are planned components until implemented and verified.
- After each session, create or update one local Markdown file in `learning-notes/`, named `session-NN-short-title.md`. Keep updates for the same session in the same file.
- Teach what was actually implemented: concepts, data flow, changed files, design decisions, commands to run, verification results, limitations, debugging guidance, and review questions. Explain terminology for a learner.
- Keep `learning-notes/` ignored by Git. Never stage it or force-add its contents.
- Run checks appropriate to the session, report actual outcomes, and distinguish implemented, unverified, and planned behavior. Never invent measurements.
- At each completed session, commit the relevant code, tests, and project documentation. Check staged changes before committing; exclude secrets, local notes, and unrelated user changes. Report the commit and any failed or unavailable checks.
- Start with restricted local development and account for authentication, redaction, and tenant isolation as relevant features are introduced.
- Verify current dependency documentation and compatibility when selecting versions or implementing evolving protocols.
