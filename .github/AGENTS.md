# GitHub Actions operations

## Self-hosted macOS runner

Native macOS jobs use the repo-specific runner labeled `mac-mini`:

- `ci.yml` routes the Swift build/test job to `[self-hosted, mac-mini]`.
  Fork pull requests stay on GitHub-hosted infrastructure; only same-repository
  pull requests and trusted pushes can use the Mini.
- `release-macapp.yml` routes the signed, notarized macOS app release to
  `[self-hosted, mac-mini]`.
- `release.yml` routes only the Darwin CLI matrix entries to the Mini.
  Linux and Windows artifacts remain on GitHub-hosted runners.

The Mini runner runs in the logged-in Aqua session. Its job environment uses:

```text
JAVA_HOME=/opt/homebrew/opt/openjdk@21
ANDROID_HOME=/Users/nikhilanand/Library/Android/sdk
ANDROID_SDK_ROOT=/Users/nikhilanand/Library/Android/sdk
DEVELOPER_DIR=/Volumes/NVMe/Xcode.app/Contents/Developer
```

The external NVMe volume must be mounted. Each Xcode job also checks the
`DEVELOPER_DIR` path before building. There is one Mini runner, so native jobs
queue rather than running concurrently.

### Signing

The Mini's logged-in `login` keychain already contains the Developer ID
Application identity. Do not add `apple-actions/import-codesign-certs` to these
Mini jobs: importing the same certificate into a temporary keychain can produce
an ambiguous signing identity. Notarization still uses the repository secrets
`APPLE_ID`, `APPLE_TEAM_ID`, and `APPLE_APP_SPECIFIC_PASSWORD`.

If signing reports `errSecInternalComponent`, unlock the login keychain once
from an interactive Terminal in the Mini's logged-in GUI session, then grant
the signing tools access:

```bash
security unlock-keychain "$HOME/Library/Keychains/login.keychain-db"
security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
  "$HOME/Library/Keychains/login.keychain-db"
```

Enter the password only at the interactive prompt. Never place it in a
workflow, command argument, environment variable, or log. Do not guess an
empty password and do not import a duplicate P12.

### Operations

The runner is installed separately from runners for other repositories:

```bash
ssh nikhilanand@192.168.1.11
cd ~/actions-runner-nikships-skills-registry
./svc.sh status
./svc.sh stop
./svc.sh start
```

Check registration from a machine with `gh`:

```bash
gh api repos/nikships/skills-registry/actions/runners
```

Never retarget, stop, remove, or reconfigure another repository's runner,
LaunchAgent, work directory, or registration. In particular, leave
`~/actions-runner` and the `ghostty-vibe-xr` LaunchAgent untouched.
