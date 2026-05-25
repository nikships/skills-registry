'use client';

/* eslint-disable react/no-unescaped-entities, react/jsx-no-comment-textnodes, @next/next/no-img-element */
import React, { useState, useEffect } from "react";

export default function Home() {
  const [configTab, setConfigTab] = useState("cfg-claude");
  const [installTab, setInstallTab] = useState("inst-curl");

  useEffect(() => {
    const handleAnchorClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      const anchor = target.closest('a');
      if (anchor && anchor.hash && anchor.hash.startsWith('#')) {
        const id = anchor.hash.slice(1);
        const el = document.getElementById(id);
        if (el) {
          e.preventDefault();
          const top = el.getBoundingClientRect().top + window.scrollY - 72;
          window.scrollTo({ top, behavior: 'smooth' });
        }
      }
    };
    document.addEventListener('click', handleAnchorClick);
    return () => document.removeEventListener('click', handleAnchorClick);
  }, []);

  return (
    <>
      <header className="topnav" data-od-id="topnav">
    <div className="container topnav-inner">
      <a href="#" className="brand-mark" aria-label="skills-registry">
        <img src="assets/logo.png" alt="skills-registry" />
      </a>
      <nav>
        <a href="#how-it-works">How it works</a>
        <a href="#mcp-tools">MCP tools</a>
        <a href="#install">Install</a>
        <a href="#cli">CLI</a>
        <a href="https://github.com/anand-92/skills-registry">GitHub</a>
      </nav>
      <div className="nav-right">
        <a className="btn btn-ghost btn-sm" href="https://github.com/anand-92/skills-registry">★ Star</a>
        <a className="btn btn-primary btn-sm" href="#install">Install</a>
      </div>
    </div>
  </header>

  <main>
    {/* ─── HERO ─── */}
    <section className="hero" data-od-id="hero">
      <div className="container hero-grid">
        <div>
          <p className="eyebrow"><span className="dot"></span> v0.7.0 · Apache-2.0 · Free &amp; open source</p>
          <h1 className="h1">One GitHub repo.<br />Every AI agent.<br />Every device.</h1>
          <p className="lead">
            Your skills live in one repo you own. Access them from any device — laptop, desktop, cloud VM — via the CLI or MCP. No more copying <span className="inline-code">SKILL.md</span> files across machines. No more syncing <span className="inline-code">~/.claude</span>, <span className="inline-code">~/.cursor</span>, <span className="inline-code">~/.codex</span> folders by hand. One registry. Everywhere you work.
          </p>
          <div className="hero-cta">
            <a className="btn btn-primary btn-arrow" href="#install">Install in one command</a>
            <a className="btn btn-ghost" href="https://github.com/anand-92/skills-registry">View on GitHub</a>
          </div>
          <p className="meta-text" style={{marginTop: "20px"}}>
            <span className="num">curl … install.sh | sh</span> &nbsp;·&nbsp; needs <span className="inline-code">gh</span> + <span className="inline-code">git</span>
          </p>
        </div>

        <div className="terminal" role="img" aria-label="Terminal showing skills-registry install">
          <div className="terminal-bar">
            <span className="dot term-dot-r"></span>
            <span className="dot term-dot-y"></span>
            <span className="dot term-dot-g"></span>
            <span className="tt">~ / skills-registry — zsh</span>
          </div>
          <div className="terminal-body">
            <span className="term-line"><span className="term-prompt">$</span> <span className="term-cmd">curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh</span></span>

            <span className="term-line term-comment"># Detecting platform…</span>
            <span className="term-line"><span className="term-ok">✓</span> darwin/arm64 detected</span>
            <span className="term-line"><span className="term-ok">✓</span> Downloaded skills-registry_darwin_arm64.tar.gz — 4.2 MB</span>
            <span className="term-line"><span className="term-ok">✓</span> Installed binary → <span className="term-accent">~/.local/bin/skills-registry</span></span>

            <span className="term-line group"><span className="term-prompt">$</span> <span className="term-cmd">skills-registry</span></span>
            <span className="term-line term-comment"># Onboarding wizard — scanning ~/.* for existing skills…</span>
            <span className="term-line"><span className="term-indent"></span>found 11 skills in <span className="term-accent">~/.claude/skills</span></span>
            <span className="term-line"><span className="term-indent"></span>found  6 skills in <span className="term-accent">~/.cursor/skills</span></span>
            <span className="term-line"><span className="term-ok">✓</span> Created <span className="term-accent">anand-92/my-skills</span> · pushed 17 skills</span>

            <span className="term-line group"><span className="term-ok">Done.</span> Wire up the hosted MCP:</span>
            <span className="term-line term-comment"># Paste into Claude Code / Cursor / VS Code mcp.json</span>
            <span className="term-line"><span className="term-warn">&#123;</span></span>
            <span className="term-line"><span className="term-warn">  "mcpServers": &#123;</span></span>
            <span className="term-line"><span className="term-warn">    "skills-registry": &#123;</span></span>
            <span className="term-line"><span className="term-warn">      "url": "https://mcp.skills-registry.dev/mcp"</span></span>
            <span className="term-line"><span className="term-warn">    &#125;</span></span>
            <span className="term-line"><span className="term-warn">  &#125;</span></span>
            <span className="term-line"><span className="term-warn">&#125;</span></span>
            <span className="term-line blank"></span>
            <span className="term-line term-comment"># Codex requires stdio MCP — not yet supported by the hosted server.</span>
            <span className="term-line group"><span className="term-prompt">$</span> <span className="term-caret"></span></span>
          </div>
        </div>
      </div>
    </section>

    {/* ─── STATS STRIP ─── */}
    <section id="stats" style={{paddingBlock: "64px"}}>
      <div className="container">
        <div className="stats-strip">
          <div className="stat">
            <div className="stat-num">50<small>+</small></div>
            <p className="stat-label">AI tool dot-folders auto-detected at bootstrap</p>
          </div>
          <div className="stat">
            <div className="stat-num">2</div>
            <p className="stat-label">MCP tools exposed — <span className="num">list_skills</span>, <span className="num">get_skill</span></p>
          </div>
          <div className="stat">
            <div className="stat-num">∞</div>
            <p className="stat-label">Devices — one registry accessible everywhere</p>
          </div>
          <div className="stat">
            <div className="stat-num">1</div>
            <p className="stat-label">Command to install &amp; wire up every agent on any machine</p>
          </div>
        </div>
      </div>
    </section>

    {/* ─── PROBLEM ─── */}
    <section id="problem">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> The problem</p>
          <h2 className="h2">Skills trapped on one machine. Copied by hand. Out of sync everywhere.</h2>
          <p className="lead">
            Your best code-review skill lives on your work laptop. Your React component library skill is on your desktop. Your cloud VM has neither. Every AI tool hoards its own local skills folder, and you are the human sync cable — copy-pasting SKILL.md files, rsyncing dot-folders, DMing yourself snippets. Worse, every skill gets auto-loaded into startup context whether you need it or not. You pay tokens for skills you'll never use this conversation.
          </p>
        </div>

        <div className="problem-grid">
          <div className="card problem-card">
            <span className="problem-tag">Before · local dot-folders</span>
            <h3 className="h4" style={{fontWeight: "600"}}>Duplication, drift, token bloat</h3>
            <ul className="problem-list" style={{marginTop: "14px"}}>
              <li className="bad">~/.claude/skills/code-review/SKILL.md</li>
              <li className="bad">~/.cursor/skills/code-review/SKILL.md</li>
              <li className="bad">~/.codex/skills/code-review/SKILL.md</li>
              <li className="bad">~/.factory/skills/code-review/SKILL.md</li>
              <li className="neu">…and 46 other agents you forgot about</li>
              <li className="bad">All loaded on every startup. Every conversation.</li>
            </ul>
          </div>

          <div className="card problem-card" style={{borderColor: "color-mix(in oklab, var(--accent) 32%, var(--border))"}}>
            <span className="problem-tag" style={{color: "var(--accent)"}}>After · skills-registry</span>
            <h3 className="h4" style={{fontWeight: "600"}}>One repo. Every device. Fetched on demand.</h3>
            <ul className="problem-list" style={{marginTop: "14px"}}>
              <li className="good">anand-92/my-skills/code-review/SKILL.md</li>
              <li className="good">Same skills on laptop, desktop, and cloud VM</li>
              <li className="good">Every agent on every device points to the same repo</li>
              <li className="good">Edit once — every machine sees the update instantly</li>
              <li className="good">Pointer file in each agent's dot-folder (~200 bytes)</li>
              <li className="good">Real skill fetched only when needed</li>
            </ul>
          </div>
        </div>
      </div>
    </section>

    {/* ─── FEATURES ─── */}
    <section id="how-it-works">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> How it works</p>
          <h2 className="h2">Single source of truth. Six deliberate constraints.</h2>
          <p className="lead">
            Every design decision falls out of one constraint: the MCP server runs in a stripped container — no shell PATH, no SSH agent, no <span className="inline-code">git config user.email</span>. So it doesn't depend on any of them.
          </p>
        </div>

        <div className="features-grid">
          <div className="feature-cell card">
            <span className="feature-num">01</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M12 2v20M5 9l7-7 7 7M5 15l7 7 7-7"/></svg></div>
            <h4 className="h4">One registry, every device</h4>
            <p>Install the CLI on any machine and point it at the same GitHub repo. Your skills follow you — laptop, desktop, remote server, or fresh VM. No manual syncing, no drift between devices.</p>
          </div>

          <div className="feature-cell card">
            <span className="feature-num">02</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg></div>
            <h4 className="h4">Fetched on demand</h4>
            <p>Tiny pointer file in each agent's dot-folder. The actual skill is downloaded the moment <span className="inline-code">get_skill(slug)</span> is called — and not before. No startup-token tax, no bloat.</p>
          </div>

          <div className="feature-cell card">
            <span className="feature-num">03</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M4 6h16M4 12h16M4 18h16"/><circle cx="6" cy="6" r="1.6" fill="currentColor"/></svg></div>
            <h4 className="h4">gh-only GitHub I/O</h4>
            <p>No <span className="inline-code">git</span> shell-out. No SSH. No embedded HTTP. Every call routes through the user's authenticated GitHub CLI via the Git Data API.</p>
          </div>

          <div className="feature-cell card">
            <span className="feature-num">04</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M21 12a9 9 0 1 1-9-9"/><path d="M21 3v6h-6"/></svg></div>
            <h4 className="h4">Tree-SHA cache</h4>
            <p>Skills cached in <span className="inline-code">~/.cache/skills-mcp/skills/</span>. Cache key is GitHub's tree SHA — force-pushes and subtree changes invalidate correctly.</p>
          </div>

          <div className="feature-cell card">
            <span className="feature-num">05</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M12 22s8-4 8-12V4l-8-2-8 2v6c0 8 8 12 8 12Z"/><path d="m9 12 2 2 4-4"/></svg></div>
            <h4 className="h4">Path-traversal hardened</h4>
            <p>Every write — <span className="inline-code">publish</span>, <span className="inline-code">add</span>, <span className="inline-code">sync</span> — rejects <span className="inline-code">..</span> segments and backslash traversals. 2 MiB cap per file. Same validation in Python and Go.</p>
          </div>

          <div className="feature-cell card">
            <span className="feature-num">06</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M6 3v18M18 3v18M3 6h18M3 18h18"/></svg></div>
            <h4 className="h4">Git, but for skills</h4>
            <p>The registry is a real GitHub repo. Branch it, PR it, fork a teammate's, restore old versions. Apache-2.0 — yours forever.</p>
          </div>
        </div>
      </div>
    </section>

    {/* ─── COMPARISON TABLE ─── */}
    <section id="compare">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> Comparison</p>
          <h2 className="h2">Why not just keep using local folders or a dotfiles repo?</h2>
        </div>

        <table className="ds-table" role="table">
          <thead>
            <tr>
              <th className="col-product" scope="col">Capability</th>
              <th scope="col" style={{textAlign: "center"}}>Local dot-folders</th>
              <th scope="col" style={{textAlign: "center"}}>Dotfiles repo</th>
              <th scope="col" className="col-us" style={{textAlign: "center"}}>skills-registry</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="feature-label">One home for every agent</td>
              <td className="cell partial">duplicated</td>
              <td className="cell yes">yes</td>
              <td className="cell yes col-us-cell">yes</td>
            </tr>
            <tr>
              <td className="feature-label">Access from any device (no manual sync)</td>
              <td className="cell no">no</td>
              <td className="cell partial">manual</td>
              <td className="cell yes col-us-cell">yes</td>
            </tr>
            <tr>
              <td className="feature-label">Fetched on demand (no startup tokens)</td>
              <td className="cell no">no</td>
              <td className="cell no">no</td>
              <td className="cell yes col-us-cell">yes</td>
            </tr>
            <tr>
              <td className="feature-label">Versioned + branchable</td>
              <td className="cell no">no</td>
              <td className="cell yes">yes</td>
              <td className="cell yes col-us-cell">yes</td>
            </tr>
            <tr>
              <td className="feature-label">Works in every MCP client</td>
              <td className="cell partial">partial</td>
              <td className="cell no">no</td>
              <td className="cell yes col-us-cell">yes</td>
            </tr>
            <tr>
              <td className="feature-label">Share / fork between users</td>
              <td className="cell no">no</td>
              <td className="cell partial">clunky</td>
              <td className="cell yes col-us-cell">clone the repo</td>
            </tr>
            <tr>
              <td className="feature-label">No shell or SSH config needed</td>
              <td className="cell yes">yes</td>
              <td className="cell no">no</td>
              <td className="cell yes col-us-cell">yes</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    {/* ─── ARCHITECTURE ─── */}
    <section id="architecture">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> Architecture</p>
          <h2 className="h2">Two deliverables. One source repo.</h2>
          <p className="lead">A hosted MCP server your agents connect to, and a Go CLI you install once. No Python to manage locally — the server runs as a service.</p>
        </div>

        <div className="arch-grid">
          <div className="card arch-card">
            <div className="arch-head">
              <span className="arch-name">skills-registry-mcp (hosted)</span>
              <span className="arch-lang">Python 3.10+</span>
            </div>
            <p className="arch-role">Hosted FastMCP at <span className="inline-code">mcp.skills-registry.dev</span>. OAuth via GitHub. Two read-only MCP tools: <span className="inline-code">list_skills</span>, <span className="inline-code">get_skill</span>. Skills served from each user's linked repo via a GitHub App installation token.</p>
            <p className="arch-dist">Hosted at mcp.skills-registry.dev · Streamable HTTP</p>
          </div>

          <div className="card arch-card">
            <div className="arch-head">
              <span className="arch-name">skills-registry</span>
              <span className="arch-lang">Go 1.24+</span>
            </div>
            <p className="arch-role">Charmbracelet TUI manager — onboarding wizard + dashboard hub for day-to-day skill management. Commands: <span className="inline-code">list</span>, <span className="inline-code">get</span>, <span className="inline-code">sync</span>, <span className="inline-code">add</span>, <span className="inline-code">publish</span>, <span className="inline-code">remove</span>.</p>
            <p className="arch-dist">GitHub Releases · darwin/linux/windows × amd64/arm64</p>
          </div>
        </div>
      </div>
    </section>

    {/* ─── MCP TOOLS + CONFIG ─── */}
    <section id="mcp-tools">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> MCP surface</p>
          <h2 className="h2">Two read-only tools. Any MCP client can call them.</h2>
        </div>

        <div className="mcp-grid">
          <div>
            <div className="tool-list">
              <div className="tool-row">
                <span className="tool-name">list_skills</span>
                <p className="tool-desc">Enumerates every skill in your linked registry. Returns a markdown table with slug, name, description, and the URI to fetch.</p>
                <span className="tool-kind">read</span>
              </div>
              <div className="tool-row">
                <span className="tool-name">get_skill(slug)</span>
                <p className="tool-desc">Returns the raw <span className="inline-code">SKILL.md</span> for one slug, served straight from your registry repo via the GitHub App installation token.</p>
                <span className="tool-kind">read</span>
              </div>
            </div>

            <p className="meta-text" style={{marginTop: "28px"}}>
              Agents call <span className="num">list_skills</span> and <span className="num">get_skill</span> on demand — you just say <em style={{color: "var(--fg)"}}>"what skills do I have?"</em> or <em style={{color: "var(--fg)"}}>"use the code-review skill on this PR"</em> and the agent picks the right tool.
            </p>
          </div>

          <div>
            <div className="code-tabs" role="tablist">
              <button className="code-tab" role="tab" aria-selected={configTab === "cfg-claude" ? "true" : "false"} onClick={() => setConfigTab("cfg-claude")}>mcp.json</button>
              <button className="code-tab" role="tab" aria-selected={configTab === "cfg-call" ? "true" : "false"} onClick={() => setConfigTab("cfg-call")}>agent call</button>
            </div>

            <div className="code-panel" id="cfg-claude" hidden={configTab !== "cfg-claude"}>
              <pre className="code-block">
<span className="c">// Claude Code / Claude Desktop / Cursor / VS Code — mcp.json</span>
<span className="p">&#123;</span>
  <span className="k">"mcpServers"</span><span className="p">:</span> <span className="p">&#123;</span>
    <span className="k">"skills-registry"</span><span className="p">:</span> <span className="p">&#123;</span>
      <span className="k">"url"</span><span className="p">:</span> <span className="s">"https://mcp.skills-registry.dev/mcp"</span>
    <span className="p">&#125;</span>
  <span className="p">&#125;</span>
<span className="p">&#125;</span></pre>
            </div>

            <div className="code-panel" id="cfg-call" hidden={configTab !== "cfg-call"}>
              <pre className="code-block">
<span className="c"># What the agent ends up doing under the hood</span>
<span className="k">user</span><span className="p">:</span> <span className="s">"use my code-review skill on this PR"</span>

<span className="k">agent</span><span className="p">:</span> get_skill<span className="p">(</span><span className="s">"code-review"</span><span className="p">)</span>
<span className="p">→</span> hosted MCP returns raw SKILL.md
<span className="p">→</span> agent reads the markdown
<span className="p">→</span> follows the skill's instructions</pre>
            </div>

            <p className="meta-text" style={{marginTop: "14px"}}><span className="num">skills-registry</span> prints this snippet for you on first run.</p>
          </div>
        </div>
      </div>
    </section>

    {/* ─── INSTALL ─── */}
    <section id="install">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> Install</p>
          <h2 className="h2">One command. Five things happen.</h2>
        </div>

        <div className="install-grid">
          <ol className="step-list">
            <li>
              <div>
                <h4>Install the CLI</h4>
                <p>One-liner: <span className="inline-code">curl … install.sh | sh</span> drops the Go binary into <span className="inline-code">~/.local/bin/skills-registry</span>.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Run <code>skills-registry</code></h4>
                <p>Bare invocation launches the onboarding wizard the first time, then routes to the dashboard hub on every subsequent run.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Wizard creates your registry repo</h4>
                <p>Scans local dot-folders, calls <span className="inline-code">gh repo create</span>, and pushes every skill it found in one <span className="inline-code">git push</span>.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Paste the MCP JSON snippet</h4>
                <p>The wizard prints a ready-to-paste <span className="inline-code">mcp.json</span> block pointing at <span className="inline-code">https://mcp.skills-registry.dev/mcp</span>. Drop it into your client config.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Install the GitHub App + restart</h4>
                <p>Authorize the <span className="inline-code">skills-registry-mcp</span> GitHub App on your registry repo, restart your MCP client, and the hosted server starts serving skills.</p>
              </div>
            </li>
          </ol>

          <div>
            <div className="code-tabs" role="tablist">
              <button className="code-tab" role="tab" aria-selected={installTab === "inst-curl" ? "true" : "false"} onClick={() => setInstallTab("inst-curl")}>Install (curl|sh)</button>
              <button className="code-tab" role="tab" aria-selected={installTab === "inst-mcp" ? "true" : "false"} onClick={() => setInstallTab("inst-mcp")}>MCP JSON</button>
            </div>

            <div className="code-panel" id="inst-curl" hidden={installTab !== "inst-curl"}>
              <pre className="code-block">
<span className="c"># Drops the Go binary into ~/.local/bin/skills-registry</span>
<span className="k">$</span> curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh

<span className="c"># Then run the wizard:</span>
<span className="k">$</span> skills-registry</pre>
            </div>

            <div className="code-panel" id="inst-mcp" hidden={installTab !== "inst-mcp"}>
              <pre className="code-block">
<span className="c">// Claude Code / Claude Desktop / Cursor / VS Code — mcp.json</span>
<span className="p">&#123;</span>
  <span className="k">"mcpServers"</span><span className="p">:</span> <span className="p">&#123;</span>
    <span className="k">"skills-registry"</span><span className="p">:</span> <span className="p">&#123;</span>
      <span className="k">"url"</span><span className="p">:</span> <span className="s">"https://mcp.skills-registry.dev/mcp"</span>
    <span className="p">&#125;</span>
  <span className="p">&#125;</span>
<span className="p">&#125;</span></pre>
            </div>

            <p className="meta-text" style={{marginTop: "14px"}}>
              Prereqs: <span className="inline-code">gh</span> authenticated (<span className="inline-code">gh auth login</span>). macOS, Linux.
            </p>
          </div>
        </div>
      </div>
    </section>

    {/* ─── CLI TABLE ─── */}
    <section id="cli">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> CLI reference</p>
          <h2 className="h2">The <span className="num" style={{color: "var(--accent)"}}>skills-registry</span> binary</h2>
          <p className="lead">Charmbracelet TUI for day-to-day management. Same Git-Data-API publish flow as the MCP server, mirrored in Go.</p>
        </div>

        <table className="cli-table">
          <thead>
            <tr><th>Command</th><th>What it does</th></tr>
          </thead>
          <tbody>
            <tr>
              <td className="cmd">skills-registry bootstrap</td>
              <td className="desc">First-run setup. Idempotent — safe to re-run.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry list</td>
              <td className="desc">Fuzzy-filterable TUI of every skill in your registry. Press <span className="inline-code">/</span> to search, Enter to preview.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry get &lt;slug&gt;</td>
              <td className="desc">Download one skill into <span className="inline-code">./skills-registry/&lt;slug&gt;/</span>.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry sync</td>
              <td className="desc">Push local skills sitting in <span className="inline-code">.claude/skills</span>, <span className="inline-code">.cursor/skills</span>, etc. that aren't yet in the registry.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry add &lt;owner/repo&gt;</td>
              <td className="desc">Clone a teammate's registry. Multi-select which of their skills to pull into your own.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry publish &lt;path&gt;</td>
              <td className="desc">Publish a single local skill folder. Path-traversal validated. 2 MiB per-file cap.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry remove &lt;slug&gt;</td>
              <td className="desc">Atomic delete — drops the slug from the registry (single Git Data API commit with null-SHA tree entries), the local cache, and every agent dot-folder.</td>
            </tr>
            <tr>
              <td className="cmd">skills-registry --version</td>
              <td className="desc">Print version. Current: <span className="num">0.7.x</span>.</td>
            </tr>
          </tbody>
        </table>

        <p className="meta-text" style={{marginTop: "20px"}}>
          Override the registry per-process with <span className="inline-code">SKILLS_REGISTRY=owner/repo</span> — useful for browsing a teammate's read-only.
        </p>
      </div>
    </section>

    {/* ─── CTA ─── */}
    <section id="cta-section">
      <div className="container">
        <div className="cta-wrap">
          <div>
            <h2>Free, open, and pre-1.0. Try it today.</h2>
            <p className="lead">Apache-2.0. No accounts. No telemetry. No paid tier — ever. Built by <a href="https://github.com/anand-92" style={{color: "#ff9ec2", textDecoration: "underline", textDecorationThickness: "1px", textUnderlineOffset: "3px"}}>anand-92</a> as an open-source dev tool. Star the repo or file an issue if it breaks.</p>
          </div>
          <div className="cta-actions">
            <a className="btn btn-light btn-arrow" href="#install"><span className="num">skills-registry</span></a>
            <a className="btn btn-outline-light" href="https://github.com/anand-92/skills-registry">★ Star on GitHub</a>
            <span className="meta-light">MCP surface stable · internals may shift between minor versions</span>
          </div>
        </div>
      </div>
    </section>
  </main>

  <footer className="pagefoot">
    <div className="container">
      <div className="foot-grid">
        <div className="foot-col">
          <a href="#" className="brand-mark foot" aria-label="skills-registry">
            <img src="assets/logo.png" alt="skills-registry" />
          </a>
          <p className="foot-tag">One GitHub repo. Every AI agent. Every device. Your skills, everywhere you work — fetched on demand.</p>
        </div>
        <div className="foot-col">
          <h5>Project</h5>
          <ul>
            <li><a href="https://github.com/anand-92/skills-registry">GitHub</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/releases">Releases</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/issues">Issues</a></li>
          </ul>
        </div>
        <div className="foot-col">
          <h5>Docs</h5>
          <ul>
            <li><a href="https://github.com/anand-92/skills-registry/blob/main/docs/registry.md">Architecture</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/blob/main/CONTRIBUTING.md">Contributing</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/blob/main/AGENTS.md">AGENTS.md</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/blob/main/SECURITY.md">Security</a></li>
          </ul>
        </div>
        <div className="foot-col">
          <h5>References</h5>
          <ul>
            <li><a href="https://modelcontextprotocol.io">Model Context Protocol</a></li>
            <li><a href="https://github.com/jlowin/fastmcp">FastMCP</a></li>
            <li><a href="https://cli.github.com/">GitHub CLI</a></li>
            <li><a href="https://github.com/astral-sh/uv">uv</a></li>
          </ul>
        </div>
      </div>

      <div className="foot-bottom">
        <span className="meta-text">© 2026 anand-92 · Apache-2.0</span>
        <span className="meta-text">v0.7.0 · Beta · MCP surface stable</span>
      </div>
    </div>
  </footer>
    </>
  );
}
