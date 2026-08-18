import type { ReactiveController, ReactiveControllerHost } from "lit";
import { z } from "zod/v4";
import {
  chatResponseSchema,
  aiOpsResponseSchema,
  uploadResponseSchema,
} from "../schemas/index.js";

export type Mode = "quick" | "stream";

export interface ChatMessage {
  type: "user" | "assistant";
  content: string;
  /** Optional step details for AI Ops results. */
  detail?: string[];
  /** A2UI protocol messages attached to an assistant reply (unknown[] at this boundary; validated per-message by the web_core schema at render time). */
  a2ui?: unknown[];
  /** Transient: reply not yet arrived — render a thinking placeholder. */
  pending?: boolean;
}

export interface ChatHistory {
  id: string;
  title: string;
  messages: ChatMessage[];
  createdAt: string;
  updatedAt: string;
}

export interface AIOpsResult {
  result: string;
  detail: string[];
  a2ui?: unknown[];
}

export type NotificationType = "info" | "success" | "warning" | "error";

export interface Notification {
  message: string;
  type: NotificationType;
}

export interface OverlayState {
  show: boolean;
  text: string;
  subtext: string;
}

const MAX_HISTORIES = 50;
const STORAGE_KEY = "swifty-agent-chat-histories";

// Zod schemas for validating the localStorage-persisted chat history shape,
// so JSON.parse results are checked instead of type-asserted.
const chatMessageSchema = z.object({
  type: z.enum(["user", "assistant"]),
  content: z.string(),
  detail: z.array(z.string()).optional(),
  a2ui: z.array(z.unknown()).optional(),
});

const chatHistorySchema = z.object({
  id: z.string(),
  title: z.string(),
  messages: z.array(chatMessageSchema),
  createdAt: z.string(),
  updatedAt: z.string(),
});

const chatHistoriesSchema = z.array(chatHistorySchema);

function generateSessionId(): string {
  return "session_" + crypto.randomUUID();
}

function loadHistories(): ChatHistory[] {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return [];
    const parsed = chatHistoriesSchema.safeParse(JSON.parse(stored));
    return parsed.success ? parsed.data : [];
  } catch {
    return [];
  }
}

function deriveTitle(messages: ChatMessage[]): string {
  const firstUser = messages.find((m) => m.type === "user");
  if (!firstUser) return "New chat";
  const c = firstUser.content;
  return c.slice(0, 30) + (c.length > 30 ? "..." : "");
}

// Lit port of the React useChat hook: a ReactiveController owning all chat
// state. Every mutation goes through #update() which persists histories and
// asks the host to re-render.
export class ChatStore implements ReactiveController {
  mode: Mode = "quick";
  sessionId: string = generateSessionId();
  isStreaming = false;
  messages: ChatMessage[] = [];
  histories: ChatHistory[] = loadHistories();
  notification: Notification | null = null;
  overlay: OverlayState = { show: false, text: "", subtext: "" };

  #host: ReactiveControllerHost;
  #streamController: AbortController | null = null;
  #notificationTimer: number | undefined;

  constructor(host: ReactiveControllerHost) {
    this.#host = host;
    host.addController(this);
  }

  hostConnected() {}

  hostDisconnected() {
    this.#streamController?.abort();
    this.#streamController = null;
    if (this.#notificationTimer) window.clearTimeout(this.#notificationTimer);
  }

  #update() {
    this.#host.requestUpdate();
  }

  #persistHistories() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.histories));
    } catch {
      // ignore quota errors
    }
  }

  setMode = (mode: Mode) => {
    this.mode = mode;
    this.#update();
  };

  showNotification = (message: string, type: NotificationType = "info") => {
    this.notification = { message, type };
    if (this.#notificationTimer) window.clearTimeout(this.#notificationTimer);
    // Auto-dismiss after 3s.
    this.#notificationTimer = window.setTimeout(() => {
      this.notification = null;
      this.#notificationTimer = undefined;
      this.#update();
    }, 3000);
    this.#update();
  };

  #upsertHistory(sid: string, msgs: ChatMessage[]) {
    if (msgs.length === 0) return;
    const title = deriveTitle(msgs);
    const now = new Date().toISOString();
    const idx = this.histories.findIndex((h) => h.id === sid);
    if (idx !== -1) {
      const updated = [...this.histories];
      updated[idx] = { ...updated[idx], messages: msgs, title, updatedAt: now };
      this.histories = updated;
    } else {
      this.histories = [
        { id: sid, title, messages: msgs, createdAt: now, updatedAt: now },
        ...this.histories,
      ].slice(0, MAX_HISTORIES);
    }
    this.#persistHistories();
    this.#update();
  }

  newChat = () => {
    if (this.isStreaming) {
      this.showNotification(
        "Please wait for the current chat to finish before starting a new one",
        "warning",
      );
      return;
    }
    this.messages = [];
    this.sessionId = generateSessionId();
    this.#update();
  };

  loadChatHistory = (id: string) => {
    const h = this.histories.find((x) => x.id === id);
    if (!h) return;
    this.sessionId = h.id;
    this.messages = h.messages;
    this.#update();
  };

  deleteChatHistory = (id: string) => {
    this.histories = this.histories.filter((h) => h.id !== id);
    this.#persistHistories();
    if (this.sessionId === id) {
      this.messages = [];
      this.sessionId = generateSessionId();
    }
    this.#update();
  };

  addMessage = (msg: ChatMessage) => {
    this.messages = [...this.messages, msg];
    this.#update();
  };

  sendMessage = async (text: string) => {
    if (!text || this.isStreaming) return;

    let currentMsgs: ChatMessage[] = [
      ...this.messages,
      { type: "user", content: text },
    ];
    this.messages = currentMsgs;
    this.isStreaming = true;
    this.#update();

    const controller = new AbortController();
    this.#streamController = controller;
    const sessionId = this.sessionId;
    const setMessages = (msgs: ChatMessage[]) => {
      this.messages = msgs;
      this.#update();
    };

    try {
      if (this.mode === "quick") {
        // Show a thinking placeholder until the reply arrives.
        currentMsgs = [
          ...currentMsgs,
          { type: "assistant", content: "", pending: true },
        ];
        setMessages(currentMsgs);
        const resp = await fetch("/api/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id: sessionId, question: text }),
          signal: controller.signal,
        });
        const parsed = chatResponseSchema.safeParse(await resp.json());
        if (!parsed.success) throw new Error("invalid chat response");
        const answer = parsed.data.data?.answer;
        const a2ui = parsed.data.data?.a2ui;
        if (parsed.data.message === "OK" && answer) {
          currentMsgs = [
            ...currentMsgs.slice(0, -1),
            {
              type: "assistant",
              content: answer,
              ...(a2ui && a2ui.length > 0 ? { a2ui } : {}),
            },
          ];
          setMessages(currentMsgs);
        } else {
          throw new Error(parsed.data.message || "Unknown error");
        }
      } else {
        const resp = await fetch("/api/chat_stream", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id: sessionId, question: text }),
          signal: controller.signal,
        });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        const reader = resp.body?.getReader();
        if (!reader) throw new Error("no stream body");
        const decoder = new TextDecoder();
        let buffer = "";
        let full = "";
        let currentEvent = "";
        let dataLines: string[] = [];
        currentMsgs = [
          ...currentMsgs,
          { type: "assistant", content: "", pending: true },
        ];
        setMessages(currentMsgs);

        // Dispatch one complete SSE event: per spec, multiple `data:` lines
        // belong to the same event and are joined with "\n" — this is how
        // newlines inside streamed chunks survive the transport.
        const dispatchEvent = () => {
          if (dataLines.length === 0) {
            currentEvent = "";
            return;
          }
          const payload = dataLines.join("\n");
          dataLines = [];
          const event = currentEvent;
          currentEvent = "";
          if (event === "message") {
            full += payload;
            const last = currentMsgs.at(-1);
            currentMsgs = [
              ...currentMsgs.slice(0, -1),
              {
                type: "assistant" as const,
                content: full,
                ...(last?.a2ui ? { a2ui: last.a2ui } : {}),
              },
            ];
            setMessages(currentMsgs);
          } else if (event === "a2ui") {
            // Payload is a JSON array of A2UI protocol messages; contents
            // are validated per-message by the web_core schema at render
            // time, so treat them as unknown[] here.
            try {
              const messages = z
                .array(z.unknown())
                .min(1)
                .safeParse(JSON.parse(payload));
              if (!messages.success)
                throw new Error("payload is not a non-empty array");
              const last = currentMsgs.at(-1);
              currentMsgs = [
                ...currentMsgs.slice(0, -1),
                {
                  type: "assistant" as const,
                  content: full,
                  a2ui: [...(last?.a2ui ?? []), ...messages.data],
                },
              ];
              setMessages(currentMsgs);
            } catch (err) {
              console.error("invalid a2ui event payload:", err);
            }
          } else if (event === "error") {
            // Surface server-side error events instead of silently ignoring.
            throw new Error(payload || "Stream error");
          }
          // "done" event: clean termination — the reader will return
          // done=true on the next read and the loop will break.
        };

        while (true) {
          // Check abort before each read so teardown cancels promptly.
          if (controller.signal.aborted) break;
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() ?? "";
          for (const rawLine of lines) {
            // Strip trailing \r for SSE spec compliance (\r\n line endings).
            const line = rawLine.endsWith("\r")
              ? rawLine.slice(0, -1)
              : rawLine;
            // Blank line terminates the current event.
            if (line === "") {
              dispatchEvent();
              continue;
            }
            if (line.startsWith("id: ")) continue;
            if (line.startsWith("event: ")) {
              currentEvent = line.slice(7);
              continue;
            }
            if (line.startsWith("data: ")) {
              dataLines.push(line.slice(6));
            } else if (line === "data:") {
              dataLines.push("");
            }
          }
        }
      }
    } catch (e) {
      // AbortError: user navigated away — suppress the error message.
      if (e instanceof DOMException && e.name === "AbortError") return;
      const msg = e instanceof Error ? e.message : String(e);
      const errorMsg: ChatMessage = {
        type: "assistant",
        content: "Error: " + msg,
      };
      // Replace a trailing thinking placeholder instead of appending after it.
      currentMsgs = currentMsgs.at(-1)?.pending
        ? [...currentMsgs.slice(0, -1), errorMsg]
        : [...currentMsgs, errorMsg];
      setMessages(currentMsgs);
    } finally {
      this.#streamController = null;
      this.isStreaming = false;
      // Clear a leftover thinking placeholder (e.g. stream ended empty).
      if (currentMsgs.at(-1)?.pending) {
        currentMsgs = currentMsgs.slice(0, -1);
        setMessages(currentMsgs);
      }
      if (!controller.signal.aborted && currentMsgs.length > 0) {
        this.#upsertHistory(sessionId, currentMsgs);
      }
      this.#update();
    }
  };

  triggerAIOps = async (): Promise<AIOpsResult | null> => {
    this.isStreaming = true;
    this.overlay = {
      show: true,
      text: "AI Ops analyzing...",
      subtext: "Backend processing, please wait",
    };
    this.#update();
    try {
      const resp = await fetch("/api/ai_ops", { method: "POST" });
      const parsed = aiOpsResponseSchema.safeParse(await resp.json());
      if (!parsed.success) throw new Error("invalid ai ops response");
      const result = parsed.data.data?.result;
      if (parsed.data.message === "OK" && result) {
        const a2ui = parsed.data.data?.a2ui;
        return {
          result,
          detail: parsed.data.data?.detail ?? [],
          ...(a2ui && a2ui.length > 0 ? { a2ui } : {}),
        };
      }
      throw new Error(parsed.data.message || "Unknown error");
    } catch (e) {
      this.showNotification(
        "AI Ops failed: " + (e instanceof Error ? e.message : String(e)),
        "error",
      );
      return null;
    } finally {
      this.isStreaming = false;
      this.overlay = { show: false, text: "", subtext: "" };
      this.#update();
    }
  };

  uploadFile = async (file: File): Promise<string | null> => {
    const allowed = [".txt", ".md", ".markdown"];
    const name = file.name.toLowerCase();
    if (!allowed.some((ext) => name.endsWith(ext))) {
      this.showNotification(
        "Only TXT or Markdown (.md) files are supported",
        "error",
      );
      return null;
    }
    if (file.size > 50 * 1024 * 1024) {
      this.showNotification("File size must not exceed 50MB", "error");
      return null;
    }
    this.isStreaming = true;
    this.overlay = {
      show: true,
      text: "Uploading file...",
      subtext: file.name,
    };
    this.#update();
    try {
      const fd = new FormData();
      fd.append("file", file);
      const resp = await fetch("/api/upload", { method: "POST", body: fd });
      const parsed = uploadResponseSchema.safeParse(await resp.json());
      if (!parsed.success) throw new Error("invalid upload response");
      if (parsed.data.message === "OK" && parsed.data.data !== undefined) {
        return `${file.name} uploaded to knowledge base`;
      }
      throw new Error(parsed.data.message || "Upload failed");
    } catch (e) {
      this.showNotification(
        "Upload failed: " + (e instanceof Error ? e.message : String(e)),
        "error",
      );
      return null;
    } finally {
      this.isStreaming = false;
      this.overlay = { show: false, text: "", subtext: "" };
      this.#update();
    }
  };
}
