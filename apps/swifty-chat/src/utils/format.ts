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
 * Human-readable file size used when uploading files in chat.
 * Mirrors the source project's `getFileSize`.
 */
export function getFileSize(size: number): string {
  if (size < 1024) return size + "B";
  if (size < 1024 * 1024) return (size / 1024).toFixed(2) + "KB";
  if (size < 1024 * 1024 * 1024) return (size / 1024 / 1024).toFixed(2) + "MB";
  return (size / 1024 / 1024 / 1024).toFixed(2) + "GB";
}

/**
 * Compact size label for the dashboard cache rows.
 */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  return (bytes / 1024).toFixed(1) + " KB";
}

/**
 * Format a cache entry expiration (stored as nanoseconds) into a
 * relative countdown string for the dashboard.
 */
export function formatExpire(nanos: number): string {
  if (nanos <= 0 || nanos >= Number.MAX_SAFE_INTEGER) return "never";
  const ms = nanos / 1_000_000;
  const now = Date.now();
  const diff = ms - now;
  if (diff <= 0) return "expired";
  if (diff < 60_000) return Math.ceil(diff / 1000) + "s left";
  if (diff < 3_600_000) return Math.ceil(diff / 60_000) + "m left";
  return new Date(ms).toLocaleTimeString();
}
