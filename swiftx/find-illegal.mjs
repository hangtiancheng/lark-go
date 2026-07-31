#!/usr/bin/env node

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { basename, resolve } from "node:path";

/**
 * @typedef {Object} MatchResult
 * @property {string} filePath - Relative path of the matched file
 * @property {number} lineNumber - 1-based line number
 * @property {number} columnNumber - 1-based column number
 * @property {string} matchedWord - The illegal word that was matched
 * @property {string} lineContent - Full content of the matched line
 */

/**
 * Case-sensitive patterns for illegal words.
 * - "Swifty" and "SWIFTY" are always illegal.
 * - "swifty" is illegal unless immediately followed by "_http" or ".go"
 *   (project names swifty_http and swifty.go are allowed).
 * @type {RegExp[]}
 */
const ILLEGAL_PATTERNS = [/Swifty/g, /SWIFTY/g, /swifty(?!_http\b|\.go\b)/g];

/**
 * Retrieve all git-tracked files under the current working directory.
 * Uses NUL-delimited output to handle special characters in paths.
 * @returns {string[]} Array of relative file paths
 */
function getTrackedFiles() {
  const output = execSync("git ls-files -z", {
    encoding: "utf-8",
    cwd: process.cwd(),
  });
  return output.split("\0").filter(Boolean);
}

/**
 * Heuristic check for binary content by looking for NUL bytes.
 * @param {Buffer} buffer - Raw file content
 * @returns {boolean} True if the file is likely binary
 */
function isBinary(buffer) {
  return buffer.includes(0);
}

/**
 * Scan a single file for all illegal word occurrences.
 * @param {string} filePath - Relative path to the file
 * @returns {MatchResult[]} All matches found in the file
 */
function scanFile(filePath) {
  /** @type {MatchResult[]} */
  const results = [];

  const buffer = readFileSync(resolve(process.cwd(), filePath));
  if (isBinary(buffer)) return results;

  const lines = buffer.toString("utf-8").split("\n");

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    for (const pattern of ILLEGAL_PATTERNS) {
      pattern.lastIndex = 0;
      /** @type {RegExpExecArray | null} */
      let match;
      while ((match = pattern.exec(line)) !== null) {
        results.push({
          filePath,
          lineNumber: i + 1,
          columnNumber: match.index + 1,
          matchedWord: match[0],
          lineContent: line,
        });
      }
    }
  }

  return results;
}

// --- Main ---

const scriptName = basename(new URL(import.meta.url).pathname);

/** @type {string[]} Files excluded from scanning */
const EXCLUDED_FILES = ["internal/remote/fe/index.js"];

const files = getTrackedFiles().filter(
  (f) => basename(f) !== scriptName && !EXCLUDED_FILES.includes(f),
);

/** @type {MatchResult[]} */
const allMatches = [];

for (const file of files) {
  allMatches.push(...scanFile(file));
}

if (allMatches.length === 0) {
  console.log("No illegal words found.");
  process.exit(0);
}

for (const m of allMatches) {
  console.log(
    `${m.filePath}:${m.lineNumber}:${m.columnNumber}: "${m.matchedWord}" in: ${m.lineContent.trim()}`,
  );
}

console.log(`\nTotal: ${allMatches.length} occurrence(s) found.`);
process.exit(1);
