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

// The package version maps directly to a release tag. Pin to "latest" only
// via SKILLS_REGISTRY_VERSION for local testing / pre-publish smoke tests.
function downloadUrl() {
  const asset = assetName();
  const version = process.env.SKILLS_REGISTRY_VERSION || `v${PKG_VERSION}`;
  if (process.env.SKILLS_REGISTRY_URL) {
    return process.env.SKILLS_REGISTRY_URL;
  }
  if (version === "latest") {
    return `https://github.com/${REPO}/releases/latest/download/${asset}`;
  }
  return `https://github.com/${REPO}/releases/download/${version}/${asset}`;
}

function get(url) {
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { "User-Agent": "skills-registry-npm" } }, (res) => {
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
  const url = downloadUrl();
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
  downloadBinary,
  isInstalled,
};
