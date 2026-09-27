// binary.js — shared helpers for the skills-registry npm launcher.
//
// This npm package ships no binary of its own. It downloads the matching
// prebuilt Go binary from the project's GitHub Releases (the same tarballs
// install.sh / install.ps1 use) and execs it. The package version is kept
// in lockstep with the CLI release tag, so version X.Y.Z of this package
// downloads the vX.Y.Z release asset.

"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");

const REPO = process.env.SKILLS_REGISTRY_REPO || "nikships/skills-registry";
const PKG_VERSION = require("../package.json").version;

// Map Node's process.platform / process.arch onto the release asset matrix.
// Supported: darwin/{amd64,arm64}, linux/{amd64,arm64}, windows/{amd64,arm64}.
const PLATFORM_MAP = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};
const ARCH_MAP = {
  x64: "amd64",
  arm64: "arm64",
};

function targetTriple() {
  const goos = PLATFORM_MAP[process.platform];
  const goarch = ARCH_MAP[process.arch];
  if (!goos || !goarch) {
    throw new Error(
      `unsupported platform: ${process.platform}/${process.arch}\n` +
        "supported: darwin/x64, darwin/arm64, linux/x64, linux/arm64, win32/x64, win32/arm64"
    );
  }
  return { goos, goarch };
}

function binaryName() {
  return process.platform === "win32" ? "skills-registry.exe" : "skills-registry";
}

// The downloaded binary lives next to this file so a global install and an
// npx cache entry each keep their own copy and `npm uninstall` cleans it up.
function binaryPath() {
  return path.join(__dirname, binaryName());
}

function assetName() {
  const { goos, goarch } = targetTriple();
  const ext = goos === "windows" ? "zip" : "tar.gz";
  return `skills-registry_${goos}_${goarch}.${ext}`;
}

// The package version maps directly to a release tag. "latest" (via
// SKILLS_REGISTRY_VERSION) is only for local testing / pre-publish smoke
// tests, and must be resolved through downloadUrlAsync: the repo also ships
// macOS app releases (macapp-v* tags) with no CLI binary, so the
// tag-agnostic /releases/latest/download endpoint 404s whenever the newest
// release overall is an app release.
function downloadUrl() {
  const asset = assetName();
  const version = process.env.SKILLS_REGISTRY_VERSION || `v${PKG_VERSION}`;
  if (process.env.SKILLS_REGISTRY_URL) {
    return process.env.SKILLS_REGISTRY_URL;
  }
  if (version === "latest") {
    throw new Error(
      'version "latest" must be resolved with downloadUrlAsync() ' +
        "(the /releases/latest/download endpoint is ambiguous across the CLI and macOS app streams)"
    );
  }
  return `https://github.com/${REPO}/releases/download/${version}/${asset}`;
}

// Async variant of downloadUrl that resolves "latest" to the newest
// published `v<digit>` release carrying the platform asset (skipping
// drafts, prereleases, and the macapp-* app stream). Pinned versions and
// SKILLS_REGISTRY_URL behave exactly like downloadUrl.
async function downloadUrlAsync() {
  const version = process.env.SKILLS_REGISTRY_VERSION || `v${PKG_VERSION}`;
  if (process.env.SKILLS_REGISTRY_URL) {
    return process.env.SKILLS_REGISTRY_URL;
  }
  if (version !== "latest") {
    return downloadUrl();
  }
  const tag = await resolveLatestTag();
  return `https://github.com/${REPO}/releases/download/${tag}/${assetName()}`;
}

// List releases newest-first and return the first published CLI-stream tag
// with a matching platform asset.
async function resolveLatestTag() {
  const asset = assetName();
  const body = await get(
    `https://api.github.com/repos/${REPO}/releases?per_page=100`,
    { Accept: "application/vnd.github+json", "User-Agent": "skills-registry-npm" }
  );
  let releases;
  try {
    releases = JSON.parse(body.toString("utf8"));
  } catch (err) {
    throw new Error(`could not parse releases for ${REPO}: ${err.message}`);
  }
  if (!Array.isArray(releases)) {
    throw new Error(`could not list releases for ${REPO}: unexpected API response`);
  }
  let sawCli = false;
  for (const r of releases) {
    if (r.draft || r.prerelease) {
      continue;
    }
    const tag = r.tag_name || "";
    if (!/^v[0-9]/.test(tag) || tag.startsWith("macapp-")) {
      continue;
    }
    sawCli = true;
    const names = (r.assets || []).map((a) => a.name);
    if (names.includes(asset)) {
      return tag;
    }
  }
  if (sawCli) {
    throw new Error(
      `no published CLI release of ${REPO} contains asset ${asset} (pin one with SKILLS_REGISTRY_VERSION)`
    );
  }
  throw new Error(
    `no published CLI release found for ${REPO} (pin one with SKILLS_REGISTRY_VERSION)`
  );
}

function get(url, headers) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: headers || { "User-Agent": "skills-registry-npm" } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          resolve(get(res.headers.location));
          return;
        }
        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`download failed: ${url} (HTTP ${res.statusCode})`));
          return;
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

// Extract a single named member from the archive using the host's bundled
// tools (tar on POSIX, PowerShell Expand-Archive on Windows). No extra deps.
function extract(archivePath, destDir) {
  const { execFileSync } = require("child_process");
  if (assetName().endsWith(".zip")) {
    execFileSync(
      "powershell",
      [
        "-NoProfile",
        "-NonInteractive",
        "-Command",
        `Expand-Archive -Path '${archivePath}' -DestinationPath '${destDir}' -Force`,
      ],
      { stdio: "inherit" }
    );
  } else {
    execFileSync("tar", ["-xzf", archivePath, "-C", destDir, binaryName()], {
      stdio: "inherit",
    });
  }
}

async function downloadBinary() {
  const url = await downloadUrlAsync();
  const dest = binaryPath();
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "skills-registry-"));
  try {
    const archivePath = path.join(tmpDir, assetName());
    const buf = await get(url);
    fs.writeFileSync(archivePath, buf);
    extract(archivePath, tmpDir);

    const extracted = path.join(tmpDir, binaryName());
    if (!fs.existsSync(extracted)) {
      throw new Error(`binary '${binaryName()}' not found inside ${assetName()}`);
    }
    fs.copyFileSync(extracted, dest);
    if (process.platform !== "win32") {
      fs.chmodSync(dest, 0o755);
    }
    return dest;
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

function isInstalled() {
  try {
    return fs.statSync(binaryPath()).size > 0;
  } catch {
    return false;
  }
}

module.exports = {
  REPO,
  PKG_VERSION,
  binaryName,
  binaryPath,
  assetName,
  downloadUrl,
  downloadUrlAsync,
  resolveLatestTag,
  downloadBinary,
  isInstalled,
};
