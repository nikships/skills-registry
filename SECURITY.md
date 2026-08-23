# Security policy

Report vulnerabilities privately through [GitHub Security Advisories](https://github.com/nikships/skills-registry/security/advisories/new). Do not open a public issue, discussion, or pull request for a vulnerability.

Include the impact, reproduction steps, `skills-registry --version`, operating system, and any logs you can safely share.

The supported surface is the latest minor release on `main`: installers and npm launcher, Go CLI, and native macOS app. Relevant reports include unsafe archive installation or update behavior, path traversal and unintended file deletion, credential exposure from local GitHub operations, malicious `SKILL.md` handling, and release supply-chain issues.

Skills are user-controlled instructions. Risks inherent to an agent consuming untrusted skill content, and issues requiring an attacker to already control the user's registry repository or local machine, are out of scope.
