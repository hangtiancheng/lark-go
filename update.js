#!/usr/bin/env node
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

/**
 * @module update
 * @description Go Workspace dependency updater.
 * Iterates over all modules defined in `go.work` and upgrades their
 * dependencies to the latest (or patch-only) versions, followed by
 * `go mod tidy` to keep go.mod / go.sum consistent.
 *
 * Usage:
 *   node update.js              # Upgrade all deps to latest
 *   node update.js --patch      # Upgrade patch versions only
 *   node update.js --dry-run    # Print commands without executing
 *   node update.js -v           # Verbose output
 */

import { execSync } from "node:child_process";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/**
 * Represents a single module entry parsed from `go work edit -json`.
 * @typedef {Object} WorkUseEntry
 * @property {string} DiskPath - Relative or absolute filesystem path to the module.
 */

/**
 * Parsed output of `go work edit -json`.
 * @typedef {Object} WorkEditJSON
 * @property {WorkUseEntry[]} Use - List of modules included in the workspace.
 */

/**
 * Resolved CLI options after parsing process.argv.
 * @typedef {Object} CLIOptions
 * @property {boolean} patchOnly - When true, pass `-u=patch` instead of `-u`.
 * @property {boolean} dryRun    - When true, print commands but do not execute.
 * @property {boolean} verbose   - When true, echo every command before execution.
 */

/**
 * Result summary for a single module update attempt.
 * @typedef {Object} ModuleResult
 * @property {string}  relPath  - Module path relative to workspace root.
 * @property {boolean} success  - Whether both `go get` and `go mod tidy` succeeded.
 * @property {string}  [error]  - Error message if the update failed.
 */

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** @type {string} Absolute path to the directory containing this script. */
const __dirname = dirname(fileURLToPath(import.meta.url));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Parse CLI arguments into a structured options object.
 * @param {string[]} argv - Raw arguments from `process.argv.slice(2)`.
 * @returns {CLIOptions} Parsed CLI options.
 */
function parseArgs(argv) {
  return {
    patchOnly: argv.includes("--patch"),
    dryRun: argv.includes("--dry-run"),
    verbose: argv.includes("--verbose") || argv.includes("-v"),
  };
}

/**
 * Read and parse the current `go.work` file via `go work edit -json`.
 * Exits the process with code 1 if parsing fails or no modules are found.
 * @returns {WorkUseEntry[]} Array of workspace module entries.
 */
function getWorkspaceModules() {
  try {
    /** @type {string} */
    const raw = execSync("go work edit -json", {
      cwd: __dirname,
      encoding: "utf-8",
    });

    /** @type {WorkEditJSON} */
    const parsed = JSON.parse(raw);

    if (!parsed.Use || parsed.Use.length === 0) {
      console.error("No modules found in go.work (Use array is empty).");
      process.exit(1);
    }

    return parsed.Use;
  } catch (err) {
    console.error(
      "Failed to parse go.work. Ensure a valid go.work exists in:",
      __dirname,
    );
    console.error(`   ${/** @type {Error} */ (err).message}`);
    process.exit(1);
  }
}

/**
 * Execute a shell command synchronously.
 * In dry-run mode the command is only printed, never executed.
 * @param {string} cmd     - Shell command to run.
 * @param {string} cwd     - Working directory for the command.
 * @param {boolean} dryRun - Skip actual execution when true.
 * @param {boolean} verbose - Echo the command before running when true.
 * @throws {Error} Re-throws execSync errors when not in dry-run mode.
 */
function run(cmd, cwd, dryRun, verbose) {
  if (verbose || dryRun) {
    console.log(`  $ ${cmd}`);
  }
  if (dryRun) return;
  execSync(cmd, { cwd, stdio: "inherit" });
}

/**
 * Convert an absolute module path to a display-friendly relative path.
 * @param {string} absPath - Absolute filesystem path.
 * @returns {string} Path relative to the workspace root, or "." for root itself.
 */
function toRelPath(absPath) {
  const prefix = __dirname + "/";
  return absPath.startsWith(prefix) ? absPath.slice(prefix.length) : ".";
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

/**
 * Entry point: iterate workspace modules, upgrade deps, and report results.
 * @returns {void}
 */
function main() {
  /** @type {CLIOptions} */
  const opts = parseArgs(process.argv.slice(2));

  /** @type {WorkUseEntry[]} */
  const modules = getWorkspaceModules();

  /** @type {string} Flag passed to `go get`. */
  const upgradeFlag = opts.patchOnly ? "-u=patch" : "-u";

  /** @type {string} Human-readable label for the current mode. */
  const modeLabel = opts.patchOnly ? "patch-only" : "latest";

  console.log(`\nGo Workspace Dependency Update [${modeLabel}]`);
  if (opts.dryRun) {
    console.log("DRY-RUN mode — no commands will be executed.\n");
  }
  console.log(`Found ${modules.length} module(s):\n`);

  /** @type {ModuleResult[]} */
  const results = [];

  for (const mod of modules) {
    /** @type {string} */
    const absPath = resolve(__dirname, mod.DiskPath);

    /** @type {string} */
    const relPath = toRelPath(absPath);

    console.log(`---------- ${relPath} ----------`);

    try {
      run(`go get ${upgradeFlag} ./...`, absPath, opts.dryRun, opts.verbose);
      run("go mod tidy", absPath, opts.dryRun, opts.verbose);
      console.log("  Done\n");
      results.push({ relPath, success: true });
    } catch (err) {
      /** @type {string} First line of the error for concise reporting. */
      const errMsg = /** @type {Error} */ (err).message.split("\n")[0];
      console.error(`  Failed: ${errMsg}\n`);
      results.push({ relPath, success: false, error: errMsg });
    }
  }

  // ── Summary Report ───────────────────────────────────────────────────────
  /** @type {ModuleResult[]} */
  const failures = results.filter((r) => !r.success);

  if (failures.length === 0) {
    console.log("All modules updated successfully!");
  } else {
    console.log(`  ${failures.length} module(s) failed:`);
    failures.forEach((f) => console.log(`  ${f.relPath}: ${f.error}`));
  }
  process.exit(failures.length > 0 ? 1 : 0);
}

main();
