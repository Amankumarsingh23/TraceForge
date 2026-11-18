"""Create a dependency inventory from actual npm audit and the unchanged lockfile."""

import json
from pathlib import Path

root = Path(__file__).resolve().parents[1]
audit = json.loads((root / "artifacts/session15/npm-current.json").read_text())
lock = json.loads((root / "web/package-lock.json").read_text())["packages"]
old = json.loads(
    (root / "docs/engineering-journal/session-14-evidence/npm-audit.json").read_text()
)
lines = [
    "# Dependency advisory review — 2026-11-18",
    "",
    "The S14 snapshot had nine moderate package findings. The current executed audit has "
    f"{audit['metadata']['vulnerabilities']['total']} package findings; these are dependency-chain reports, not that many independent vulnerabilities. "
    "All affected installed paths below belong to development tooling. The separate production audit reports zero known findings; that does not remove development risk.",
    "",
    "| Package | Actual installed versions | Direct? | Original nine? | Current severity / upstream chain | Status |",
    "| --- | --- | --- | --- | --- | --- |",
]
for name, value in audit["vulnerabilities"].items():
    versions = sorted({lock[node]["version"] for node in value["nodes"]})
    via = [
        item if isinstance(item, str) else item["url"].rsplit("/", 1)[-1]
        for item in value["via"]
    ]
    lines.append(
        f"| {name} | {', '.join(versions)} | {'yes' if value['isDirect'] else 'transitive'} | "
        f"{'yes' if name in old['vulnerabilities'] else 'no'} | {value['severity']}: {', '.join(via)} | open; restricted local dev risk accepted |"
    )
lines += [
    "",
    "## Exact advisories, usage and remediation",
    "",
    "[GHSA-g3hv-75wg-m249 / CVE-2026-118764](https://github.com/advisories/GHSA-g3hv-75wg-m249): minimatch>=7,<11.0.1 has glob preprocessing amplification when optimizationLevel>=2. "
    "Installed10.2.6 reaches ESLint config/file matching and TypeScript-ESLint project/file selection. The patched floor is11.0.1; registry latest observed11.0.9 supports Node20 or>=22, but current callers declare^10.2.x. "
    "A forced11 override would cross their supported major range. No compatible10-series patch was identified. We preserve the pinned lockfile instead of accepting an unreviewed override/downgrade. "
    "The nine original reports all inherit this advisory; some now also inherit the second advisory below.",
    "",
    "[GHSA-fc5q-39xc-vjpf / CVE-2026-112366](https://github.com/advisories/GHSA-fc5q-39xc-vjpf): picomatch<=4.0.7 has repeated-extglob ReDoS. "
    "The primary advisory reviewed2026-11-17 lists no patched version; registry latest observed4.0.7. It reaches Next lint file discovery, fast-glob/micromatch, tinyglobby, Vite and Vitest test discovery. "
    "The newly expanded chain accounts for the additional/current high findings. npm's suggested Vitest downgrade is not evidence of a safe patch and was not applied.",
    "",
    "Affected patterns come from trusted repository/config/test-discovery inputs in this supported workflow. No application request handler imports these dev glob libraries or accepts tenant-provided glob patterns. "
    "This reduces the identified runtime exposure but does not make untrusted checkouts/PR configs safe: malicious patterns can deny developer/CI CPU or memory. Keep those workflows isolated and bounded, and do not expose dev/test servers to untrusted networks. "
    "Re-review when patched compatible releases are available. Status remains OPEN, not fixed or zero-vulnerability.",
    "",
    "## Audit/install reproducibility",
    "",
    "Use npm12.2.2 `npm --prefix web audit --json` and `npm --prefix web audit --omit=dev --json`; capture both exit codes and output. "
    "The bounded final run used --fetch-retries=0 --fetch-timeout=10000 after an earlier slow registry request. npm audit exits nonzero when findings exist. "
    "A separate clean directory copied the existing package.json/package-lock.json and successfully ran `npm ci --offline --ignore-scripts --no-audit` (256 packages); it verifies cached lock resolution, not lifecycle scripts or a network-clean OS. "
    "Production compilation/tests used the installed working tree dependencies. No package versions or lockfile were changed. "
    "[npm audit semantics](https://docs.npmjs.com/cli/commands/npm-audit/) explain omitted scope and why --force is not a compatibility review.",
]
(root / "docs/final/dependency-review.md").write_text(
    "\n".join(lines) + "\n", encoding="utf-8"
)
print("Wrote exact installed-version inventory from audit and lockfile")
