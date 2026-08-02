/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

// @ts-check
"use strict";

/**
 * Cross-platform build for swifty_cli.
 *
 * Compiles ./cmd/swifty for every GOOS/GOARCH pair by setting env vars
 * and invoking `go build`. Output binaries land in swifty_cli/bin/.
 *
 * CLI usage:
 *     node scripts/build_swifty.js                       # build all platforms
 *     node scripts/build_swifty.js --dry-run             # print without building
 *     node scripts/build_swifty.js --os=darwin           # filter by OS
 *     node scripts/build_swifty.js --arch=arm64          # filter by arch
 *     node scripts/build_swifty.js --trimpath            # add -trimpath -ldflags="-s -w"
 *
 * Library usage:
 *     import { buildCross } from './build_swifty.js';
 *     buildCross({ trimpath: true, filter: { os: 'darwin' } });
 */

import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/** @type {string} Project root (directory containing this script's parent). */
const ROOT_DIR = path.dirname(path.dirname(fileURLToPath(import.meta.url)));

/** @type {string} The module being cross-compiled. */
const MODULE = "swifty_cli";

/** @type {string} The main package path relative to the module dir. */
const MAIN_PACKAGE = "./cmd/swifty";

/**
 * @typedef {object} Target
 * @property {string} os   - GOOS value (e.g. "linux", "darwin", "windows").
 * @property {string} arch - GOARCH value (e.g. "amd64", "arm64").
 */

/** @type {Target[]} All supported cross-compile targets. */
const TARGETS = [
  { os: "linux", arch: "amd64" },
  { os: "linux", arch: "arm64" },
  { os: "darwin", arch: "amd64" },
  { os: "darwin", arch: "arm64" },
  { os: "windows", arch: "amd64" },
  { os: "windows", arch: "arm64" },
];

/**
 * @typedef {object} BuildOptions
 * @property {boolean} [dryRun]    - If true, print commands without executing.
 * @property {boolean} [trimpath]  - If true, add -trimpath -ldflags="-s -w".
 * @property {string}  [go]        - Go binary path (default: "go").
 * @property {{os?: string, arch?: string}} [filter] - Filter targets by os/arch.
 */

/**
 * Compute the output binary path for a target.
 * @param {Target} target
 * @returns {string}
 */
function outputName(target) {
  const ext = target.os === "windows" ? ".exe" : "";
  const base = `swifty-${target.os}-${target.arch}${ext}`;
  return path.join(ROOT_DIR, MODULE, "bin", base);
}

/**
 * Build the go command args for a single target.
 * @param {Target} target
 * @param {boolean} trimpath
 * @returns {string[]}
 */
function buildArgs(target, trimpath) {
  /** @type {string[]} */
  const args = ["build"];
  if (trimpath) {
    args.push("-trimpath", `-ldflags=${JSON.stringify("-s -w")}`);
  }
  args.push("-o", outputName(target), MAIN_PACKAGE);
  return args;
}

/**
 * Run a cross-compile build for one target.
 * @param {Target} target
 * @param {Required<Omit<BuildOptions, "filter">>} opts
 * @returns {boolean} true on success.
 */
function buildOne(target, opts) {
  const args = buildArgs(target, opts.trimpath);
  const env = {
    ...process.env,
    GOOS: target.os,
    GOARCH: target.arch,
    CGO_ENABLED: "0",
  };
  const cwd = path.join(ROOT_DIR, MODULE);

  const label = `${target.os}/${target.arch}`;
  console.log(`==> Building ${label}`);
  console.log(`    GOOS=${target.os} GOARCH=${target.arch} ${opts.go} ${args.join(" ")}`);

  if (opts.dryRun) {
    console.log("    [DRY] skipped");
    return true;
  }

  const result = spawnSync(opts.go, args, { cwd, env, stdio: "inherit" });
  if (result.status !== 0) {
    console.error(`    [FAIL] ${label} (exit ${result.status ?? "?"})`);
    return false;
  }
  return true;
}

/**
 * Cross-compile swifty_cli for all (or filtered) targets.
 * @param {BuildOptions} [options]
 * @returns {{built: number, failed: number, skipped: number}}
 */
function buildCross(options = {}) {
  const opts = {
    dryRun: options.dryRun ?? false,
    trimpath: options.trimpath ?? false,
    go: options.go ?? process.env.GO ?? "go",
  };

  /** @type {Target[]} */
  let targets = TARGETS;
  if (options.filter) {
    targets = TARGETS.filter((t) => {
      if (options.filter?.os && t.os !== options.filter.os) return false;
      if (options.filter?.arch && t.arch !== options.filter.arch) return false;
      return true;
    });
  }

  // Ensure bin/ exists.
  const binDir = path.join(ROOT_DIR, MODULE, "bin");
  if (!opts.dryRun) {
    fs.mkdirSync(binDir, { recursive: true });
  }

  let built = 0;
  let failed = 0;
  for (const target of targets) {
    if (buildOne(target, opts)) {
      built++;
    } else {
      failed++;
    }
  }

  const skipped = TARGETS.length - targets.length;
  console.log(`\nDone. built=${built}, failed=${failed}, skipped=${skipped}, dry_run=${opts.dryRun}`);
  return { built, failed, skipped };
}

// ---- CLI -------------------------------------------------------------------

/**
 * Parse CLI arguments.
 * @param {string[]} argv
 * @returns {BuildOptions}
 */
function parseArgs(argv) {
  const args = argv.slice(2);
  /** @type {BuildOptions} */
  const result = {};
  /** @type {{os?: string, arch?: string}} */
  const filter = {};

  for (const arg of args) {
    if (arg === "--dry-run") {
      result.dryRun = true;
    } else if (arg === "--trimpath") {
      result.trimpath = true;
    } else if (arg.startsWith("--os=")) {
      filter.os = arg.slice(5);
    } else if (arg.startsWith("--arch=")) {
      filter.arch = arg.slice(7);
    } else if (arg === "--help" || arg === "-h") {
      console.log(
        `Usage: node scripts/build_swifty.js [options]\n\n` +
        `Options:\n` +
        `  --dry-run       Print commands without building\n` +
        `  --trimpath      Add -trimpath -ldflags="-s -w" for smaller binaries\n` +
        `  --os=<os>       Filter targets by GOOS (linux, darwin, windows)\n` +
        `  --arch=<arch>   Filter targets by GOARCH (amd64, arm64)\n` +
        `  --help, -h      Show this help\n\n` +
        `Targets: ${TARGETS.map((t) => `${t.os}/${t.arch}`).join(", ")}`,
      );
      process.exit(0);
    }
  }

  if (filter.os || filter.arch) {
    result.filter = filter;
  }
  return result;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  const result = buildCross(parseArgs(process.argv));
  process.exit(result.failed > 0 ? 1 : 0);
}

export { buildCross, parseArgs, outputName, buildArgs, TARGETS, MODULE, MAIN_PACKAGE };
