import { Semaphore } from "es-toolkit";

import { file } from "./api";
import type { UploadedFile } from "./schemas";

/** The backend rejects any chunk larger than 10 MiB. */
const CHUNK_SIZE = 5 * 1024 * 1024;
const MAX_PARALLEL_CHUNKS = 3;
/** `/file/verify` validates ext_name against this, dot excluded. */
const EXT_PATTERN = /^[a-zA-Z0-9]{1,10}$/;

export interface UploadProgress {
  completed: number;
  total: number;
}

function extNameOf(fileName: string): string {
  const ext = fileName.includes(".") ? (fileName.split(".").pop() ?? "") : "";
  return EXT_PATTERN.test(ext) ? ext.toLowerCase() : "bin";
}

async function sha256Hex(blob: Blob): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    await blob.arrayBuffer(),
  );
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

/** Hash-verify, upload only the chunks the server is still missing, then merge. */
export async function uploadInChunks(
  source: File,
  onProgress?: (progress: UploadProgress) => void,
  signal?: AbortSignal,
): Promise<UploadedFile> {
  const extName = extNameOf(source.name);
  const fileHash = await sha256Hex(source);
  const total = Math.max(1, Math.ceil(source.size / CHUNK_SIZE));

  const verified = await file.verify({
    file_hash: fileHash,
    chunk_cnt: total,
    ext_name: extName,
  });

  if (verified.uploaded) {
    onProgress?.({ completed: total, total });
    return {
      url: verified.url,
      file_name: source.name,
      file_size: String(source.size),
    };
  }

  let completed = total - verified.pending_chunks.length;
  onProgress?.({ completed, total });

  const gate = new Semaphore(MAX_PARALLEL_CHUNKS);
  await Promise.all(
    verified.pending_chunks.map(async (index) => {
      await gate.acquire();
      try {
        const start = index * CHUNK_SIZE;
        await file.uploadChunk(
          { file_hash: fileHash, ext_name: extName, chunk_idx: index },
          source.slice(start, start + CHUNK_SIZE),
          signal,
        );
        completed += 1;
        onProgress?.({ completed, total });
      } finally {
        gate.release();
      }
    }),
  );

  return file.merge({
    file_hash: fileHash,
    ext_name: extName,
    file_name: source.name,
  });
}
