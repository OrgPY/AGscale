#!/usr/bin/env node
/* eslint-disable no-console */
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const https = require("node:https");
const { spawnSync } = require("node:child_process");

const PKG = require("../package.json");

const DEFAULT_REPO = (PKG.agscale && PKG.agscale.releaseRepo) || "OrgPY/agscale-releases";
const MAX_REDIRECTS = 5;

function mapPlatform() {
  const p = process.platform;
  if (p === "linux") return "linux";
  if (p === "darwin") return "darwin";
  if (p === "win32") return "windows";
  throw new Error(`unsupported platform: ${p}`);
}

function mapArch() {
  const a = process.arch;
  if (a === "x64") return "amd64";
  if (a === "arm64") return "arm64";
  throw new Error(`unsupported architecture: ${a}`);
}

function envValue(...names) {
  for (const name of names) {
    const v = process.env[name];
    if (v && v.trim()) return v.trim();
  }
  return "";
}

function getTag() {
  const envTag = envValue("AGSCALE_RELEASE_TAG");
  if (envTag) return envTag.startsWith("v") ? envTag : `v${envTag}`;
  if (PKG.version === "0.0.0-dev") {
    throw new Error("package version is 0.0.0-dev; set AGSCALE_RELEASE_TAG=vX.Y.Z");
  }
  return `v${PKG.version}`;
}

function getRepo() {
  // AGSCALE_REPO is kept for backwards compatibility with <=0.1.4.
  return envValue("AGSCALE_RELEASE_REPO", "AGSCALE_REPO") || DEFAULT_REPO;
}

function request(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    // Only send the token to GitHub itself, never to redirected asset hosts.
    const headers = { "User-Agent": "agscale-npm-wrapper" };
    if (process.env.GITHUB_TOKEN && new URL(url).hostname === "github.com") {
      headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
    }
    const req = https.request(url, { method: "GET", headers }, (res) => {
      const status = res.statusCode || 0;
      if (status >= 300 && status < 400 && res.headers.location) {
        res.resume();
        if (redirects >= MAX_REDIRECTS) {
          reject(new Error(`too many redirects: ${url}`));
          return;
        }
        resolve(request(new URL(res.headers.location, url).toString(), redirects + 1));
        return;
      }
      if (status < 200 || status >= 300) {
        res.resume();
        reject(new Error(`request failed ${status}: ${url}`));
        return;
      }
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolve(Buffer.concat(chunks)));
      res.on("error", reject);
    });
    req.on("error", reject);
    req.end();
  });
}

function sha256(buf) {
  return crypto.createHash("sha256").update(buf).digest("hex");
}

// Matches goreleaser's name_template: {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
// where .Version has no leading "v".
function binaryFilename(version, osName, arch) {
  const base = `agscale_${version}_${osName}_${arch}`;
  return osName === "windows" ? `${base}.exe` : base;
}

async function ensureBinary() {
  const repo = getRepo();
  const tag = getTag();
  const version = tag.replace(/^v/, "");
  const osName = mapPlatform();
  const arch = mapArch();
  const assetName = binaryFilename(version, osName, arch);

  const cacheRoot = process.env.AGSCALE_CACHE_DIR || path.join(os.homedir(), ".cache", "agscale-npm");
  const cacheDir = path.join(cacheRoot, repo.replace("/", "_"), tag, `${osName}-${arch}`);
  const binPath = path.join(cacheDir, osName === "windows" ? "agscale.exe" : "agscale");
  if (fs.existsSync(binPath)) return binPath;

  fs.mkdirSync(cacheDir, { recursive: true });

  const base = `https://github.com/${repo}/releases/download/${tag}`;
  const [binBuf, checksumsBuf] = await Promise.all([
    request(`${base}/${assetName}`),
    request(`${base}/checksums.txt`)
  ]);
  const expected = checksumsBuf
    .toString("utf8")
    .split("\n")
    .map((l) => l.trim().split(/\s+/))
    .find((parts) => parts.length >= 2 && parts[1] === assetName);
  if (!expected) {
    throw new Error(`checksum entry missing for asset ${assetName}`);
  }
  if (sha256(binBuf) !== expected[0]) {
    throw new Error(`checksum mismatch for ${assetName}`);
  }

  // Write to a temp file and rename so an interrupted download never leaves a
  // truncated binary in the cache.
  const tmpPath = `${binPath}.${process.pid}.tmp`;
  fs.writeFileSync(tmpPath, binBuf, { mode: 0o755 });
  fs.renameSync(tmpPath, binPath);
  return binPath;
}

async function main() {
  const args = process.argv.slice(2);
  try {
    const bin = await ensureBinary();
    const child = spawnSync(bin, args, { stdio: "inherit" });
    if (child.error) throw child.error;
    if (child.signal) {
      process.kill(process.pid, child.signal);
      return;
    }
    process.exit(child.status === null ? 1 : child.status);
  } catch (err) {
    console.error(`[agscale npm wrapper] ${err.message}`);
    process.exit(1);
  }
}

main();
