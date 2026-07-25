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

// First: cd resolve(__filepath, ../../../../../swiftx-cli)
// Second: run `pnpm install`, then `pnpm fe:build`
// Third: cp dist (/path/to/swiftx-cli/apps/swiftx/src/remote/fe/dist) to dirname(__filepath) (/path/to/swiftx.go/swiftx_cli/internal/remote/fe)

import path from "node:path";
import fs from "node:fs";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/** @type {string} Directory containing this script. */
const __dirname = path.dirname(fileURLToPath(import.meta.url));

/** @type {string} Root of the swiftx-cli repository (sibling of swiftx.go). */
const swiftxCliRoot = path.resolve(__dirname, "../../../../..", "swiftx-cli");

/** @type {string} Package dir that owns the `fe:build` script. */
const swiftxAppDir = path.join(swiftxCliRoot, "apps", "swiftx");

/** @type {string} Build output produced by `pnpm fe:build`. */
const distDir = path.join(swiftxAppDir, "src", "remote", "fe", "dist");

/** @type {string} Destination dist dir next to this script. */
const targetDir = path.join(__dirname, "dist");

/**
 * Run a command synchronously, inheriting stdio; exit on failure.
 * @param {string} command - Executable to run.
 * @param {string[]} args - Command arguments.
 * @param {string} cwd - Working directory for the command.
 * @returns {void}
 */
function run(command, args, cwd) {
  console.log(`\n$ ${command} ${args.join(" ")}  (cwd: ${cwd})`);
  const result = spawnSync(command, args, {
    cwd,
    stdio: "inherit",
    shell: process.platform === "win32",
  });
  if (result.error) {
    console.error(`Failed to run ${command}:`, result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) {
    console.error(
      `${command} ${args.join(" ")} exited with code ${result.status}`,
    );
    process.exit(result.status ?? 1);
  }
}

/**
 * Build the swiftx-cli frontend and copy its dist into this directory.
 * @returns {void}
 */
function main() {
  if (!fs.existsSync(swiftxCliRoot)) {
    console.error(`swiftx-cli repo not found at ${swiftxCliRoot}`);
    process.exit(1);
  }

  run("pnpm", ["install"], swiftxCliRoot);
  run("pnpm", ["fe:build"], swiftxAppDir);

  if (!fs.existsSync(distDir)) {
    console.error(`Build output not found at ${distDir}`);
    process.exit(1);
  }

  if (fs.existsSync(targetDir)) {
    console.log(`Removing old dist at ${targetDir}`);
    fs.rmSync(targetDir, { recursive: true, force: true });
  }
  fs.cpSync(distDir, targetDir, { recursive: true });
  console.log(`\nCopied ${distDir} -> ${targetDir}`);
}

main();
