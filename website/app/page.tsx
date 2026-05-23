'use client';

import React, { useState, useEffect } from "react";

export default function Home() {
  const [configTab, setConfigTab] = useState("cfg-claude");
  const [installTab, setInstallTab] = useState("inst-uvx");

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
      <a href="#" className="brand-mark" aria-label="skill-registry">
        <img src="assets/logo.png" alt="skill-registry" />
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
          <p className="eyebrow"><span className="dot"></span> v0.5.0 · Apache-2.0 · Free &amp; open source</p>
          <h1 className="h1">One GitHub repo.<br />Every AI agent.</h1>
          <p className="lead">
            Stop copy-pasting <span className="inline-code">SKILL.md</span> files into <span className="inline-code">~/.claude</span>, <span className="inline-code">~/.cursor</span>, <span className="inline-code">~/.codex</span>. Skills live in one repo you own. Agents fetch them on demand over MCP — no startup-token tax, no drift.
          </p>
          <div className="hero-cta">
            <a className="btn btn-primary btn-arrow" href="#install">Install in one command</a>
            <a className="btn btn-ghost" href="https://github.com/anand-92/skills-registry">View on GitHub</a>
          </div>
          <p className="meta-text" style={{marginTop: "20px"}}>
            <span className="num">uvx skills-registry init</span> &nbsp;·&nbsp; needs <span className="inline-code">gh</span> + <span className="inline-code">uv</span>
          </p>
        </div>

        <div className="terminal" role="img" aria-label="Terminal showing skills-registry init">
          <div className="terminal-bar">
            <span className="dot term-dot-r"></span>
            <span className="dot term-dot-y"></span>
            <span className="dot term-dot-g"></span>
            <span className="tt">~ / skills-registry — zsh</span>
          </div>
          <div className="terminal-body">
            <span className="term-line"><span className="term-prompt">$</span> <span className="term-cmd">uvx skills-registry init</span></span>

            <span className="term-line term-comment"># Verifying prerequisites…</span>
            <span className="term-line"><span className="term-ok">✓</span> gh authenticated as <span className="term-accent">anand-92</span></span>
            <span className="term-line"><span className="term-ok">✓</span> Downloaded skills-registry (darwin/arm64) — 4.2 MB</span>
            <span className="term-line"><span className="term-ok">✓</span> Handoff → <span className="term-accent">skill-registry bootstrap</span></span>

            <span className="term-line term-comment"># Scanning ~/.* for existing skills…</span>
            <span className="term-line"><span className="term-indent"></span>found 11 skills in <span className="term-accent">~/.claude/skills</span></span>
            <span className="term-line"><span className="term-indent"></span>found  6 skills in <span className="term-accent">~/.cursor/skills</span></span>
            <span className="term-line"><span className="term-indent"></span>found  3 skills in <span className="term-accent">~/.factory/skills</span></span>

            <span className="term-line term-comment"># Creating registry repo…</span>
            <span className="term-line"><span className="term-indent"></span>repo: <span className="term-accent">anand-92/my-skills</span> (private)</span>
            <span className="term-line"><span className="term-ok">✓</span> Created repo · pushed 20 skills</span>
            <span className="term-line"><span className="term-ok">✓</span> Wired 7 agents · wrote SKILL.md pointers</span>

            <span className="term-line group"><span className="term-ok">Done.</span> Paste this into your MCP client:</span>
            <span className="term-line term-comment"># Claude Code / Claude Desktop / Cursor / VS Code — mcp.json</span>
            <span className="term-line"><span className="term-warn">&#123;</span></span>
            <span className="term-line"><span className="term-warn">  "mcpServers": &#123;</span></span>
            <span className="term-line"><span className="term-warn">    "skill-registry": &#123;</span></span>
            <span className="term-line"><span className="term-warn">      "command": "skill-registry-mcp"</span></span>
            <span className="term-line"><span className="term-warn">    &#125;</span></span>
            <span className="term-line"><span className="term-warn">  &#125;</span></span>
            <span className="term-line"><span className="term-warn">&#125;</span></span>
            <span className="term-line blank"></span>
            <span className="term-line term-comment"># Codex — ~/.codex/config.toml</span>
            <span className="term-line"><span className="term-warn">[mcp_servers.skill-registry]</span></span>
            <span className="term-line"><span className="term-warn">command = "skill-registry-mcp"</span></span>
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
            <div className="stat-num">3</div>
            <p className="stat-label">MCP tools exposed — <span className="num">list_skills</span>, <span className="num">get_skill</span>, <span className="num">publish_skill</span></p>
          </div>
          <div className="stat">
            <div className="stat-num">0</div>
            <p className="stat-label">SSH keys, git configs, or shell PATHs required</p>
          </div>
          <div className="stat">
            <div className="stat-num">1</div>
            <p className="stat-label">Command to install &amp; wire up every agent</p>
          </div>
        </div>
      </div>
    </section>

    {/* ─── PROBLEM ─── */}
    <section id="problem">
      <div className="container">
        <div className="section-head">
          <p className="eyebrow"><span className="dot"></span> The problem</p>
          <h2 className="h2">Every AI tool wants its own skills folder. Edit once, sync N times.</h2>
          <p className="lead">
            Today every AI coding tool keeps a local skills folder. Same skill, copy-pasted into N dot-folders. Worse — every skill in every folder gets auto-loaded into the agent's startup context, whether the current task needs it or not. You pay tokens for skills you'll never use this conversation.
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
            <h3 className="h4" style={{fontWeight: "600"}}>One repo. Fetched on demand.</h3>
            <ul className="problem-list" style={{marginTop: "14px"}}>
              <li className="good">anand-92/my-skills/code-review/SKILL.md</li>
              <li className="good">Every agent points to the same repo</li>
              <li className="good">Edit once · branch · PR · fork · restore</li>
              <li className="good">Pointer file in each agent's dot-folder (~200 bytes)</li>
              <li className="good">Real skill fetched only when needed</li>
              <li className="good">Cache invalidated on tree SHA change</li>
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
            Every design decision falls out of one observation: desktop MCP clients spawn the server with a stripped environment — no shell PATH, no SSH agent, no <span className="inline-code">git config user.email</span>. So we don't depend on any of them.
          </p>
        </div>

        <div className="features-grid">
          <div className="feature-cell card">
            <span className="feature-num">01</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M12 2v20M5 9l7-7 7 7M5 15l7 7 7-7"/></svg></div>
            <h4 className="h4">Fetched on demand</h4>
            <p>Tiny pointer file in each agent's dot-folder. The actual skill is downloaded the moment <span className="inline-code">get_skill(slug)</span> is called — and not before.</p>
          </div>

          <div className="feature-cell card">
            <span className="feature-num">02</span>
            <div className="feature-mark"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg></div>
            <h4 className="h4">50+ tools recognized</h4>
            <p>Claude Code, Claude Desktop, Cursor, Codex, Windsurf, Goose, Factory, VS Code/Copilot — all detected at bootstrap, universal ones pre-selected.</p>
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
            <p><span className="inline-code">publish_skill</span> rejects <span className="inline-code">..</span> segments and backslash traversals. Per-file size cap (2 MiB). Identical validation in Python &amp; Go.</p>
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
          <h2 className="h2">Three deliverables. One source repo.</h2>
          <p className="lead">Python bootstrap + Python MCP server + Go TUI manager. The Python wheel ships both Python entry points; the Go binary is downloaded from GitHub Releases on first run.</p>
        </div>

        <div className="arch-grid">
          <div className="card arch-card">
            <div className="arch-head">
              <span className="arch-name">skills-registry</span>
              <span className="arch-lang">Python 3.10+</span>
            </div>
            <p className="arch-role">Thin bootstrap. Verifies <span className="inline-code">gh</span>, downloads the Go CLI, <span className="inline-code">exec</span>s it. One command: <span className="inline-code">skills-registry init</span>.</p>
            <p className="arch-dist">PyPI wheel · entry point</p>
          </div>

          <div className="card arch-card">
            <div className="arch-head">
              <span className="arch-name">skill-registry-mcp</span>
              <span className="arch-lang">Python 3.10+</span>
            </div>
            <p className="arch-role">FastMCP server exposing <span className="inline-code">list_skills</span>, <span className="inline-code">get_skill</span>, <span className="inline-code">publish_skill</span> over MCP stdio. Same wheel, second entry point.</p>
            <p className="arch-dist">PyPI wheel · MCP stdio · FastMCP 3.x</p>
          </div>

          <div className="card arch-card">
            <div className="arch-head">
              <span className="arch-name">skill-registry</span>
              <span className="arch-lang">Go 1.24+</span>
            </div>
            <p className="arch-role">Charmbracelet TUI manager. Commands: <span className="inline-code">bootstrap</span>, <span className="inline-code">list</span>, <span className="inline-code">get</span>, <span className="inline-code">sync</span>, <span className="inline-code">add</span>, <span className="inline-code">publish</span>.</p>
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
          <h2 className="h2">Three tools. Any MCP-aware agent can call them.</h2>
        </div>

        <div className="mcp-grid">
          <div>
            <div className="tool-list">
              <div className="tool-row">
                <span className="tool-name">list_skills</span>
                <p className="tool-desc">Enumerates every skill in the registry. Returns a markdown table with slug, name, description, and the URI to fetch.</p>
                <span className="tool-kind">read</span>
              </div>
              <div className="tool-row">
                <span className="tool-name">get_skill(slug)</span>
                <p className="tool-desc">Downloads one skill into the local cache. Returns the absolute path. Cache invalidated on tree-SHA change.</p>
                <span className="tool-kind">read</span>
              </div>
              <div className="tool-row">
                <span className="tool-name">publish_skill(name, …)</span>
                <p className="tool-desc">Publishes a skill via the GitHub Git Data API. Accepts <span className="inline-code">files</span> mapping or <span className="inline-code">local_folder</span>. Returns commit SHA. 3 retries on conflict.</p>
                <span className="tool-kind write">write</span>
              </div>
            </div>

            <p className="meta-text" style={{marginTop: "28px"}}>
              Users don't call these directly. They just say <em style={{color: "var(--fg)"}}>"what skills do I have?"</em> or <em style={{color: "var(--fg)"}}>"use the code-review skill on this PR"</em> — the agent picks the right tool.
            </p>
          </div>

          <div>
            <div className="code-tabs" role="tablist">
              <button className="code-tab" role="tab" aria-selected={configTab === "cfg-claude" ? "true" : "false"} onClick={() => setConfigTab("cfg-claude")}>mcp.json</button>
              <button className="code-tab" role="tab" aria-selected={configTab === "cfg-codex" ? "true" : "false"} onClick={() => setConfigTab("cfg-codex")}>codex toml</button>
              <button className="code-tab" role="tab" aria-selected={configTab === "cfg-call" ? "true" : "false"} onClick={() => setConfigTab("cfg-call")}>agent call</button>
            </div>

            <div className="code-panel" id="cfg-claude" hidden={configTab !== "cfg-claude"}>
              <pre className="code-block">
<span className="c">// Claude Code / Claude Desktop / Cursor / VS Code — mcp.json</span>
<span className="p">&#123;</span>
  <span className="k">"mcpServers"</span><span className="p">:</span> <span className="p">&#123;</span>
    <span className="k">"skill-registry"</span><span className="p">:</span> <span className="p">&#123;</span>
      <span className="k">"command"</span><span className="p">:</span> <span className="s">"skill-registry-mcp"</span>
    <span className="p">&#125;</span>
  <span className="p">&#125;</span>
<span className="p">&#125;</span></pre>
            </div>

            <div className="code-panel" id="cfg-codex" hidden={configTab !== "cfg-codex"}>
              <pre className="code-block">
<span className="c"># ~/.codex/config.toml</span>
<span className="p">[</span><span className="k">mcp_servers.skill-registry</span><span className="p">]</span>
<span className="k">command</span> = <span className="s">"skill-registry-mcp"</span></pre>
            </div>

            <div className="code-panel" id="cfg-call" hidden={configTab !== "cfg-call"}>
              <pre className="code-block">
<span className="c"># What the agent ends up doing under the hood</span>
<span className="k">user</span><span className="p">:</span> <span className="s">"use my code-review skill on this PR"</span>

<span className="k">agent</span><span className="p">:</span> get_skill<span className="p">(</span><span className="s">"code-review"</span><span className="p">)</span>
<span className="p">→</span> <span className="v">/Users/you/.cache/skills-mcp/skills/code-review/</span>
<span className="p">→</span> reads SKILL.md
<span className="p">→</span> follows the skill's instructions</pre>
            </div>

            <p className="meta-text" style={{marginTop: "14px"}}><span className="num">skills-registry init</span> prints the platform-correct snippet for you.</p>
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
                <h4>Verify <code>gh</code> is authed</h4>
                <p>Exits with <span className="inline-code">code 3</span> if missing, <span className="inline-code">code 4</span> if not logged in. Run <span className="inline-code">gh auth login</span> first.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Download the Go CLI</h4>
                <p>Pulls the matching tarball from GitHub Releases into <span className="inline-code">~/.local/bin</span> (or <span className="inline-code">$SKILLS_BIN_DIR</span>).</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Hand off → <code>skill-registry bootstrap</code></h4>
                <p>Scans <span className="inline-code">~/.*</span> for known dot-folders. Pre-selects universal ones (factory, codex). You confirm the multi-select.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Create the registry repo</h4>
                <p>Calls <span className="inline-code">gh repo create</span>. Pushes every found skill via the Git Data API. Writes <span className="inline-code">SKILL.md</span> pointer files into each agent.</p>
              </div>
            </li>
            <li>
              <div>
                <h4>Print the MCP snippet</h4>
                <p>Platform-correct JSON / TOML you paste into your MCP client. Restart the client. Done. Re-running <span className="inline-code">init</span> is safe — idempotent.</p>
              </div>
            </li>
          </ol>

          <div>
            <div className="code-tabs" role="tablist">
              <button className="code-tab" role="tab" aria-selected={installTab === "inst-uvx" ? "true" : "false"} onClick={() => setInstallTab("inst-uvx")}>uvx (recommended)</button>
              <button className="code-tab" role="tab" aria-selected={installTab === "inst-uv" ? "true" : "false"} onClick={() => setInstallTab("inst-uv")}>uv tool</button>
              <button className="code-tab" role="tab" aria-selected={installTab === "inst-pip" ? "true" : "false"} onClick={() => setInstallTab("inst-pip")}>pip</button>
            </div>

            <div className="code-panel" id="inst-uvx" hidden={installTab !== "inst-uvx"}>
              <pre className="code-block">
<span className="c"># No system Python needed — uvx handles it</span>
<span className="k">$</span> uvx skills-registry init

<span className="c"># Then in your MCP client:</span>
<span className="k">$</span> cat ~/.config/claude/mcp.json</pre>
            </div>

            <div className="code-panel" id="inst-uv" hidden={installTab !== "inst-uv"}>
              <pre className="code-block">
<span className="c"># Install persistently — MCP clients won't depend on uvx cache</span>
<span className="k">$</span> uv tool install skills-registry

<span className="c"># Installs both entry points:</span>
<span className="c">#   skills-registry            (bootstrap CLI)</span>
<span className="c">#   skill-registry-mcp         (MCP stdio server)</span>

<span className="k">$</span> skills-registry init</pre>
            </div>

            <div className="code-panel" id="inst-pip" hidden={installTab !== "inst-pip"}>
              <pre className="code-block">
<span className="c"># Classic pip — needs Python 3.10+ already on PATH</span>
<span className="k">$</span> pip install skills-registry
<span className="k">$</span> skills-registry init

<span className="c"># Prerequisites checked at runtime:</span>
<span className="c">#   - gh        (https://cli.github.com/)</span>
<span className="c">#   - gh auth login already run</span></pre>
            </div>

            <p className="meta-text" style={{marginTop: "14px"}}>
              Prereqs: <span className="inline-code">gh</span> + <span className="inline-code">uv</span>. macOS, Linux, Windows (best-effort).
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
          <h2 className="h2">The <span className="num" style={{color: "var(--accent)"}}>skill-registry</span> binary</h2>
          <p className="lead">Charmbracelet TUI for day-to-day management. Same Git-Data-API publish flow as the MCP server, mirrored in Go.</p>
        </div>

        <table className="cli-table">
          <thead>
            <tr><th>Command</th><th>What it does</th></tr>
          </thead>
          <tbody>
            <tr>
              <td className="cmd">skill-registry bootstrap</td>
              <td className="desc">First-run setup. Idempotent — safe to re-run.</td>
            </tr>
            <tr>
              <td className="cmd">skill-registry list</td>
              <td className="desc">Fuzzy-filterable TUI of every skill in your registry. Press <span className="inline-code">/</span> to search, Enter to preview.</td>
            </tr>
            <tr>
              <td className="cmd">skill-registry get &lt;slug&gt;</td>
              <td className="desc">Download one skill into <span className="inline-code">./skill-registry/&lt;slug&gt;/</span>.</td>
            </tr>
            <tr>
              <td className="cmd">skill-registry sync</td>
              <td className="desc">Push local skills sitting in <span className="inline-code">.claude/skills</span>, <span className="inline-code">.cursor/skills</span>, etc. that aren't yet in the registry.</td>
            </tr>
            <tr>
              <td className="cmd">skill-registry add &lt;owner/repo&gt;</td>
              <td className="desc">Clone a teammate's registry. Multi-select which of their skills to pull into your own.</td>
            </tr>
            <tr>
              <td className="cmd">skill-registry publish &lt;path&gt;</td>
              <td className="desc">Publish a single local skill folder. Path-traversal validated. 2 MiB per-file cap.</td>
            </tr>
            <tr>
              <td className="cmd">skill-registry --version</td>
              <td className="desc">Print version. Current: <span className="num">0.5.0</span>.</td>
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
            <a className="btn btn-light btn-arrow" href="#install"><span className="num">uvx skills-registry init</span></a>
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
          <a href="#" className="brand-mark foot" aria-label="skill-registry">
            <img src="assets/logo.png" alt="skill-registry" />
          </a>
          <p className="foot-tag">One GitHub repo, every AI agent. Skills fetched on demand — not auto-loaded into every startup context.</p>
        </div>
        <div className="foot-col">
          <h5>Project</h5>
          <ul>
            <li><a href="https://github.com/anand-92/skills-registry">GitHub</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/releases">Releases</a></li>
            <li><a href="https://github.com/anand-92/skills-registry/issues">Issues</a></li>
            <li><a href="https://pypi.org/project/skills-registry/">PyPI</a></li>
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
        <span className="meta-text">v0.5.0 · Beta · MCP surface stable</span>
      </div>
    </div>
  </footer>
    </>
  );
}
