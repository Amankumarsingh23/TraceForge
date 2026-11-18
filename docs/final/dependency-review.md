# Dependency advisory review — 2026-11-18

The S14 snapshot had nine moderate package findings. The current executed audit has 17 package findings; these are dependency-chain reports, not that many independent vulnerabilities. All affected installed paths below belong to development tooling. The separate production audit reports zero known findings; that does not remove development risk.

| Package | Actual installed versions | Direct? | Original nine? | Current severity / upstream chain | Status |
| --- | --- | --- | --- | --- | --- |
| @eslint/config-array | 0.23.5 | transitive | yes | moderate: minimatch | open; restricted local dev risk accepted |
| @next/eslint-plugin-next | 16.4.6 | yes | no | high: fast-glob | open; restricted local dev risk accepted |
| @typescript-eslint/eslint-plugin | 8.72.2 | transitive | yes | moderate: @typescript-eslint/type-utils, @typescript-eslint/utils | open; restricted local dev risk accepted |
| @typescript-eslint/parser | 8.72.2 | transitive | yes | moderate: @typescript-eslint/typescript-estree | open; restricted local dev risk accepted |
| @typescript-eslint/type-utils | 8.72.2 | transitive | yes | moderate: @typescript-eslint/typescript-estree, @typescript-eslint/utils | open; restricted local dev risk accepted |
| @typescript-eslint/typescript-estree | 8.72.2 | transitive | yes | high: minimatch, tinyglobby | open; restricted local dev risk accepted |
| @typescript-eslint/utils | 8.72.2 | transitive | yes | moderate: @typescript-eslint/typescript-estree | open; restricted local dev risk accepted |
| eslint | 10.14.0 | yes | yes | moderate: minimatch | open; restricted local dev risk accepted |
| fast-glob | 3.3.1 | transitive | no | high: micromatch | open; restricted local dev risk accepted |
| fdir | 6.5.0 | transitive | no | high: picomatch | open; restricted local dev risk accepted |
| micromatch | 4.0.8 | transitive | no | high: picomatch | open; restricted local dev risk accepted |
| minimatch | 10.2.6 | transitive | yes | moderate: GHSA-g3hv-75wg-m249 | open; restricted local dev risk accepted |
| picomatch | 2.3.2, 4.0.7 | transitive | no | high: GHSA-fc5q-39xc-vjpf | open; restricted local dev risk accepted |
| tinyglobby | 0.2.17 | transitive | no | high: fdir, picomatch | open; restricted local dev risk accepted |
| typescript-eslint | 8.72.2 | yes | yes | moderate: @typescript-eslint/eslint-plugin, @typescript-eslint/parser, @typescript-eslint/typescript-estree, @typescript-eslint/utils | open; restricted local dev risk accepted |
| vite | 8.3.8 | transitive | no | high: picomatch, tinyglobby | open; restricted local dev risk accepted |
| vitest | 5.1.0 | yes | no | high: picomatch, tinyglobby, vite | open; restricted local dev risk accepted |

## Exact advisories, usage and remediation

[GHSA-g3hv-75wg-m249 / CVE-2026-118764](https://github.com/advisories/GHSA-g3hv-75wg-m249): minimatch>=7,<11.0.1 has glob preprocessing amplification when optimizationLevel>=2. Installed10.2.6 reaches ESLint config/file matching and TypeScript-ESLint project/file selection. The patched floor is11.0.1; registry latest observed11.0.9 supports Node20 or>=22, but current callers declare^10.2.x. A forced11 override would cross their supported major range. No compatible10-series patch was identified. We preserve the pinned lockfile instead of accepting an unreviewed override/downgrade. The nine original reports all inherit this advisory; some now also inherit the second advisory below.

[GHSA-fc5q-39xc-vjpf / CVE-2026-112366](https://github.com/advisories/GHSA-fc5q-39xc-vjpf): picomatch<=4.0.7 has repeated-extglob ReDoS. The primary advisory reviewed2026-11-17 lists no patched version; registry latest observed4.0.7. It reaches Next lint file discovery, fast-glob/micromatch, tinyglobby, Vite and Vitest test discovery. The newly expanded chain accounts for the additional/current high findings. npm's suggested Vitest downgrade is not evidence of a safe patch and was not applied.

Affected patterns come from trusted repository/config/test-discovery inputs in this supported workflow. No application request handler imports these dev glob libraries or accepts tenant-provided glob patterns. This reduces the identified runtime exposure but does not make untrusted checkouts/PR configs safe: malicious patterns can deny developer/CI CPU or memory. Keep those workflows isolated and bounded, and do not expose dev/test servers to untrusted networks. Re-review when patched compatible releases are available. Status remains OPEN, not fixed or zero-vulnerability.

## Audit/install reproducibility

Use npm12.2.2 `npm --prefix web audit --json` and `npm --prefix web audit --omit=dev --json`; capture both exit codes and output. The bounded final run used --fetch-retries=0 --fetch-timeout=10000 after an earlier slow registry request. npm audit exits nonzero when findings exist. A separate clean directory copied the existing package.json/package-lock.json and successfully ran `npm ci --offline --ignore-scripts --no-audit` (256 packages); it verifies cached lock resolution, not lifecycle scripts or a network-clean OS. Production compilation/tests used the installed working tree dependencies. No package versions or lockfile were changed. [npm audit semantics](https://docs.npmjs.com/cli/commands/npm-audit/) explain omitted scope and why --force is not a compatibility review.
