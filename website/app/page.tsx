/* eslint-disable react/no-unescaped-entities, @next/next/no-img-element */

const repo = "https://github.com/nikships/skills-registry";

export default function Home() {
  return (
    <>
      <header className="topnav">
        <div className="container topnav-inner">
          <a href="#top" className="brand-mark" aria-label="Skills Registry home">
            <img src="assets/logo.png" alt="Skills Registry" />
          </a>
          <nav aria-label="Main navigation">
            <a href="#how-it-works">How it works</a>
            <a href="#cli">CLI</a>
            <a href="#gateway">Gateway skill</a>
            <a href="#mac-app">macOS</a>
            <a href={repo}>GitHub</a>
          </nav>
          <div className="nav-right">
            <a className="btn btn-ghost btn-sm" href={repo}>★ Star</a>
            <a className="btn btn-primary btn-sm" href="#install">Install</a>
          </div>
        </div>
      </header>

      <main id="top">
        <section className="hero">
          <div className="container hero-grid">
            <div>
              <p className="eyebrow"><span className="dot" /> Apache-2.0 · Free &amp; open source</p>
              <h1 className="h1">One GitHub repo.<br />Every AI agent.<br />Every device.</h1>
              <p className="lead">
                Keep every <span className="inline-code">SKILL.md</span> in a GitHub repo you own. Manage it with a fast Go CLI and TUI, give agents a tiny gateway skill, or use the native macOS app. No hand-copying dot-folders between machines.
              </p>
              <div className="hero-cta">
                <a className="btn btn-primary btn-arrow" href="#install">Install in one command</a>
                <a className="btn btn-ghost" href={repo}>View on GitHub</a>
              </div>
              <p className="meta-text" style={{ marginTop: 20 }}>
                GitHub-backed · works with private repos · needs <span className="inline-code">gh</span> + <span className="inline-code">git</span>
              </p>
            </div>

            <div className="terminal" role="img" aria-label="Terminal showing Skills Registry installation and onboarding">
              <div className="terminal-bar">
                <span className="dot term-dot-r" /><span className="dot term-dot-y" /><span className="dot term-dot-g" />
                <span className="tt">~ / skills-registry — zsh</span>
              </div>
              <div className="terminal-body">
                <span className="term-line"><span className="term-prompt">$</span> <span className="term-cmd">curl -fsSL https://raw.githubusercontent.com/nikships/skills-registry/main/install.sh | sh</span></span>
                <span className="term-line"><span className="term-ok">✓</span> Installed → <span className="term-accent">~/.local/bin/skills-registry</span></span>
                <span className="term-line group"><span className="term-prompt">$</span> <span className="term-cmd">skills-registry</span></span>
                <span className="term-line term-comment"># scanning local agent folders…</span>
                <span className="term-line"><span className="term-indent" />found 17 skills across 3 agents</span>
                <span className="term-line"><span className="term-ok">✓</span> Created <span className="term-accent">nikships/my-skills</span></span>
                <span className="term-line"><span className="term-ok">✓</span> Pushed 17 skills in one commit</span>
                <span className="term-line"><span className="term-ok">✓</span> Installed the gateway skill for your agents</span>
                <span className="term-line group"><span className="term-prompt">$</span> <span className="term-cmd">skills-registry search "code review"</span></span>
                <span className="term-line"><span className="term-accent">code-review</span>  Review pull requests systematically</span>
                <span className="term-line group"><span className="term-prompt">$</span> <span className="term-cmd">skills-registry get code-review</span></span>
                <span className="term-line"><span className="term-ok">✓</span> Downloaded <span className="term-accent">code-review/SKILL.md</span></span>
                <span className="term-line group"><span className="term-prompt">$</span> <span className="term-caret" /></span>
              </div>
            </div>
          </div>
        </section>

        <section id="stats" style={{ paddingBlock: 64 }}>
          <div className="container"><div className="stats-strip">
            <div className="stat"><div className="stat-num">50<small>+</small></div><p className="stat-label">Agent dot-folders recognized during discovery</p></div>
            <div className="stat"><div className="stat-num">10</div><p className="stat-label">Headless commands plus an interactive dashboard</p></div>
            <div className="stat"><div className="stat-num">3</div><p className="stat-label">Ways to work: CLI/TUI, gateway skill, and macOS app</p></div>
            <div className="stat"><div className="stat-num">1</div><p className="stat-label">GitHub repo remains your source of truth</p></div>
          </div></div>
        </section>

        <section id="problem">
          <div className="container">
            <div className="section-head">
              <p className="eyebrow"><span className="dot" /> The problem</p>
              <h2 className="h2">Skills trapped in local folders drift.</h2>
              <p className="lead">A skill changes on your laptop but not your desktop or remote machine. Each agent keeps another copy. Skills Registry makes the repo—not a pile of dot-folders—the durable source.</p>
            </div>
            <div className="problem-grid">
              <div className="card problem-card">
                <span className="problem-tag">Before · copied everywhere</span>
                <h3 className="h4">Duplication and drift</h3>
                <ul className="problem-list">
                  <li className="bad">~/.claude/skills/code-review</li><li className="bad">~/.cursor/skills/code-review</li><li className="bad">~/.codex/skills/code-review</li><li className="neu">Repeat on every machine</li>
                </ul>
              </div>
              <div className="card problem-card accent-card">
                <span className="problem-tag accent-text">After · one registry</span>
                <h3 className="h4">Versioned, searchable, portable</h3>
                <ul className="problem-list">
                  <li className="good">One GitHub repo you control</li><li className="good">CLI access from any authenticated machine</li><li className="good">A gateway tells agents how to search and fetch</li><li className="good">Normal Git history, branches, forks, and PRs</li>
                </ul>
              </div>
            </div>
          </div>
        </section>

        <section id="how-it-works">
          <div className="container">
            <div className="section-head">
              <p className="eyebrow"><span className="dot" /> How it works</p>
              <h2 className="h2">GitHub is the registry. Your tools are the clients.</h2>
              <p className="lead">Reads use a local shallow mirror; writes create normal commits through GitHub.</p>
            </div>
            <div className="features-grid">
              {[
                ["01", "Discover", "The wizard scans known agent folders and finds existing skills."],
                ["02", "Bootstrap", "Create a private or public GitHub repo and push the initial skill tree."],
                ["03", "Manage", "Browse, search, add, sync, publish, remove, and update from the TUI or commands."],
                ["04", "Read quickly", "A shallow local Git mirror makes repeat listing and fetching fast."],
                ["05", "Delegate", "The gateway skill teaches compatible agents to invoke the CLI only when a skill is needed."],
                ["06", "Work anywhere", "Point another laptop, desktop, or remote machine at the same repository."],
              ].map(([number, title, body]) => (
                <div className="feature-cell card" key={number}><span className="feature-num">{number}</span><h4 className="h4">{title}</h4><p>{body}</p></div>
              ))}
            </div>
          </div>
        </section>

        <section id="architecture">
          <div className="container">
            <div className="section-head">
              <p className="eyebrow"><span className="dot" /> Architecture</p>
              <h2 className="h2">One registry. Three focused interfaces.</h2>
              <p className="lead">Choose the terminal, let an agent follow the gateway, or use a native desktop interface. They share the same GitHub-backed model.</p>
            </div>
            <div className="arch-grid">
              <div className="card arch-card"><div className="arch-head"><span className="arch-name">skills-registry</span><span className="arch-lang">Go</span></div><p className="arch-role">Charmbracelet onboarding wizard, dashboard TUI, and scriptable JSON-capable commands.</p><p className="arch-dist">GitHub Releases · npm launcher · macOS/Linux/Windows</p></div>
              <div className="card arch-card"><div className="arch-head"><span className="arch-name">gateway skill</span><span className="arch-lang">SKILL.md</span></div><p className="arch-role">A small instruction file installed into agent folders. It delegates registry search and retrieval to the CLI instead of duplicating every skill.</p><p className="arch-dist">Generated by bootstrap · readable and editable</p></div>
              <div className="card arch-card"><div className="arch-head"><span className="arch-name">Skills Registry.app</span><span className="arch-lang">SwiftUI</span></div><p className="arch-role">Native browsing, Markdown previews, fuzzy search, publishing, removal, and bulk import.</p><p className="arch-dist">macOS · Apple Silicon</p></div>
            </div>
          </div>
        </section>

        <section id="gateway">
          <div className="container gateway-grid">
            <div className="section-head">
              <p className="eyebrow"><span className="dot" /> Gateway skill</p>
              <h2 className="h2">A small pointer, not another copy.</h2>
              <p className="lead">Bootstrap installs a human-readable gateway into selected agent folders. It tells the agent to search your registry and fetch the matching skill through the CLI when needed.</p>
            </div>
            <pre className="code-block"><code>{`# Agent workflow
skills-registry search "review this pull request" --json
skills-registry get code-review --json

# The repository remains the source of truth.
# The requested skill is materialized only when needed.`}</code></pre>
          </div>
        </section>

        <section id="cli">
          <div className="container">
            <div className="section-head"><p className="eyebrow"><span className="dot" /> CLI + TUI</p><h2 className="h2">Interactive for people. Headless for scripts and agents.</h2><p className="lead">Run the binary without arguments for the wizard or dashboard. Every subcommand also supports structured output with <span className="inline-code">--json</span>.</p></div>
            <figure className="media-frame"><img src="assets/hub.gif" alt="Skills Registry dashboard with Manage, Sync, Add, Publish, Purge, and Settings" /><figcaption className="meta-text">The dashboard hub opens every day-to-day workflow.</figcaption></figure>
            <table className="cli-table"><thead><tr><th>Command</th><th>What it does</th></tr></thead><tbody>
              <tr><td className="cmd">skills-registry</td><td className="desc">Launch onboarding on first run, then the dashboard.</td></tr>
              <tr><td className="cmd">skills-registry list / search</td><td className="desc">Browse all skills or fuzzy-rank a query.</td></tr>
              <tr><td className="cmd">skills-registry get &lt;slug&gt;</td><td className="desc">Fetch one skill from the configured registry.</td></tr>
              <tr><td className="cmd">skills-registry sync</td><td className="desc">Publish newly discovered local skills.</td></tr>
              <tr><td className="cmd">skills-registry add &lt;owner/repo&gt;</td><td className="desc">Select skills from another GitHub registry.</td></tr>
              <tr><td className="cmd">skills-registry publish / remove</td><td className="desc">Commit a skill update or remove a registry entry.</td></tr>
              <tr><td className="cmd">skills-registry update</td><td className="desc">Update the installed CLI binary.</td></tr>
            </tbody></table>
          </div>
        </section>

        <section id="mac-app">
          <div className="container">
            <div className="section-head"><p className="eyebrow"><span className="dot" /> Native macOS app</p><h2 className="h2">The same registry, without the terminal.</h2><p className="lead">The Apple Silicon SwiftUI app supports GitHub login, rich Markdown browsing, fuzzy search, publishing, removal, bulk local import, and one-click CLI installation.</p></div>
            <figure className="media-frame"><img src="assets/mac-app.png" alt="Skills Registry macOS app with skill list, rendered Markdown, and file browser" /></figure>
          </div>
        </section>

        <section id="compare">
          <div className="container">
            <div className="section-head"><p className="eyebrow"><span className="dot" /> Comparison</p><h2 className="h2">Purpose-built for skills, built on ordinary Git.</h2></div>
            <table className="ds-table"><thead><tr><th className="col-product">Capability</th><th>Local folders</th><th>Dotfiles repo</th><th className="col-us">Skills Registry</th></tr></thead><tbody>
              <tr><td className="feature-label">One source for every agent</td><td className="cell no">no</td><td className="cell yes">yes</td><td className="cell yes col-us-cell">yes</td></tr>
              <tr><td className="feature-label">Guided discovery and import</td><td className="cell no">no</td><td className="cell no">no</td><td className="cell yes col-us-cell">yes</td></tr>
              <tr><td className="feature-label">Search and fetch on demand</td><td className="cell no">no</td><td className="cell partial">manual</td><td className="cell yes col-us-cell">yes</td></tr>
              <tr><td className="feature-label">Branches, forks, and PRs</td><td className="cell no">no</td><td className="cell yes">yes</td><td className="cell yes col-us-cell">yes</td></tr>
              <tr><td className="feature-label">Interactive TUI and native app</td><td className="cell no">no</td><td className="cell no">no</td><td className="cell yes col-us-cell">yes</td></tr>
              <tr><td className="feature-label">Structured automation output</td><td className="cell no">no</td><td className="cell no">no</td><td className="cell yes col-us-cell">--json</td></tr>
            </tbody></table>
          </div>
        </section>

        <section id="install">
          <div className="container install-grid">
            <div>
              <div className="section-head"><p className="eyebrow"><span className="dot" /> Install</p><h2 className="h2">From zero to your own registry.</h2></div>
              <ol className="step-list">
                <li><div><h4>Install the Go CLI</h4><p>Use the shell installer, PowerShell installer, or npm launcher.</p></div></li>
                <li><div><h4>Run <code>skills-registry</code></h4><p>The wizard discovers local skills and asks where your registry should live.</p></div></li>
                <li><div><h4>Create and populate the repo</h4><p>Authenticate with GitHub CLI, choose visibility, and push the initial tree.</p></div></li>
                <li><div><h4>Select your agents</h4><p>Install the gateway skill into the agent folders you use.</p></div></li>
              </ol>
            </div>
            <div>
              <pre className="code-block"><code>{`# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/nikships/skills-registry/main/install.sh | sh

# npm launcher
npx skills-registry

# Windows PowerShell
irm https://raw.githubusercontent.com/nikships/skills-registry/main/install.ps1 | iex`}</code></pre>
              <p className="meta-text" style={{ marginTop: 14 }}>Prerequisites: <span className="inline-code">gh auth login</span> and Git.</p>
            </div>
          </div>
        </section>

        <section id="cta-section"><div className="container"><div className="cta-wrap"><div><h2>Your skills. Your repo. Your workflow.</h2><p className="lead">No account, telemetry, or paid tier. Inspect the source, open an issue, and keep your registry wherever you choose on GitHub.</p></div><div className="cta-actions"><a className="btn btn-light btn-arrow" href="#install">Install Skills Registry</a><a className="btn btn-outline-light" href={repo}>★ Star on GitHub</a><span className="meta-light">Open source · Apache-2.0</span></div></div></div></section>
      </main>

      <footer className="pagefoot"><div className="container"><div className="foot-grid">
        <div className="foot-col"><a href="#top" className="brand-mark foot"><img src="assets/logo.png" alt="Skills Registry" /></a><p className="foot-tag">A GitHub-backed home for agent skills, with a Go CLI/TUI, gateway skill, and native macOS app.</p></div>
        <div className="foot-col"><h5>Project</h5><ul><li><a href={repo}>GitHub</a></li><li><a href={`${repo}/releases`}>Releases</a></li><li><a href={`${repo}/issues`}>Issues</a></li></ul></div>
        <div className="foot-col"><h5>Documentation</h5><ul><li><a href={`${repo}#readme`}>Getting started</a></li><li><a href={`${repo}/blob/main/docs/registry.md`}>Architecture</a></li><li><a href={`${repo}/blob/main/CONTRIBUTING.md`}>Contributing</a></li><li><a href={`${repo}/blob/main/SECURITY.md`}>Security</a></li></ul></div>
        <div className="foot-col"><h5>Tools</h5><ul><li><a href="https://cli.github.com/">GitHub CLI</a></li><li><a href="https://git-scm.com/">Git</a></li><li><a href={`${repo}/tree/main/mac-app`}>macOS app</a></li><li><a href="https://www.npmjs.com/package/skills-registry">npm package</a></li></ul></div>
      </div><div className="foot-bottom"><span className="meta-text">© 2026 nikships · Apache-2.0</span><span className="meta-text">GitHub-backed · local-first · open source</span></div></div></footer>
    </>
  );
}
