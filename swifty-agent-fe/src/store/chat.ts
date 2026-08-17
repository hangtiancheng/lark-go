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

// DEAD CODE!!!

import { create } from "zustand";
import type { ChatMessage, ChatHistoryItem } from "../types";

interface ChatState {
  sessionId: string;
  messages: ChatMessage[];
  isStreaming: boolean;
  currentMode: "quick" | "stream";
  chatHistories: ChatHistoryItem[];
  isCentered: boolean;
  showLoading: boolean;

  setSessionId: (id: string) => void;
  addMessage: (msg: ChatMessage) => void;
  updateLastAssistantMessage: (content: string) => void;
  updateLastAssistantDetails: (details: string[]) => void;
  setStreaming: (streaming: boolean) => void;
  setMode: (mode: "quick" | "stream") => void;
  clearMessages: () => void;
  newSession: () => void;
  saveCurrentChat: () => void;
  loadChatHistory: (historyId: string) => void;
  deleteChatHistory: (historyId: string) => void;
  setCentered: (centered: boolean) => void;
  setShowLoading: (show: boolean) => void;
}

function generateSessionId(): string {
  return (
    "session_" + Math.random().toString(36).substring(2, 11) + "_" + Date.now()
  );
}

function loadHistoriesFromStorage(): ChatHistoryItem[] {
  try {
    const stored = localStorage.getItem("swifty_agent_histories");
    return stored ? JSON.parse(stored) : [];
  } catch {
    return [];
  }
}

function saveHistoriesToStorage(histories: ChatHistoryItem[]) {
  try {
    localStorage.setItem("swifty_agent_histories", JSON.stringify(histories));
  } catch {
    // Silently fail if storage is full.
  }
}

const useChatStore = create<ChatState>((set, get) => ({
  sessionId: generateSessionId(),
  messages: [],
  isStreaming: false,
  currentMode: "quick",
  chatHistories: loadHistoriesFromStorage(),
  isCentered: true,
  showLoading: false,

  setSessionId(id: string) {
    set({ sessionId: id });
  },

  addMessage(msg: ChatMessage) {
    set((state) => ({
      messages: [...state.messages, msg],
      isCentered: false,
    }));
  },

  updateLastAssistantMessage(content: string) {
    set((state) => {
      const msgs = [...state.messages];
      for (let i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].type === "assistant") {
          msgs[i] = { ...msgs[i], content };
          break;
        }
      }
      return { messages: msgs };
    });
  },

  updateLastAssistantDetails(details: string[]) {
    set((state) => {
      const msgs = [...state.messages];
      for (let i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].type === "assistant") {
          msgs[i] = { ...msgs[i], details };
          break;
        }
      }
      return { messages: msgs };
    });
  },

  setStreaming(streaming: boolean) {
    set({ isStreaming: streaming });
  },

  setMode(mode: "quick" | "stream") {
    set({ currentMode: mode });
  },

  clearMessages() {
    set({ messages: [], isCentered: true });
  },

  newSession() {
    const state = get();
    if (state.messages.length > 0) {
      state.saveCurrentChat();
    }
    set({
      sessionId: generateSessionId(),
      messages: [],
      isStreaming: false,
      currentMode: "quick",
      isCentered: true,
    });
  },

  saveCurrentChat() {
    const state = get();
    if (state.messages.length === 0) return;

    const histories = [...state.chatHistories];
    const existingIdx = histories.findIndex((h) => h.id === state.sessionId);
    const firstUserMsg = state.messages.find((m) => m.type === "user");
    const title = firstUserMsg
      ? firstUserMsg.content.substring(0, 30) +
        (firstUserMsg.content.length > 30 ? "..." : "")
      : "New chat";

    const entry: ChatHistoryItem = {
      id: state.sessionId,
      title,
      messages: [...state.messages],
      createdAt:
        existingIdx >= 0
          ? histories[existingIdx].createdAt
          : new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    if (existingIdx >= 0) {
      histories[existingIdx] = entry;
    } else {
      histories.unshift(entry);
    }

    const trimmed = histories.slice(0, 50);
    saveHistoriesToStorage(trimmed);
    set({ chatHistories: trimmed });
  },

  loadChatHistory(historyId: string) {
    const state = get();
    if (state.messages.length > 0 && state.sessionId !== historyId) {
      state.saveCurrentChat();
    }

    const history = state.chatHistories.find((h) => h.id === historyId);
    if (!history) return;

    set({
      sessionId: history.id,
      messages: [...history.messages],
      isCentered: history.messages.length === 0,
    });
  },

  deleteChatHistory(historyId: string) {
    const state = get();
    const histories = state.chatHistories.filter((h) => h.id !== historyId);
    saveHistoriesToStorage(histories);

    if (state.sessionId === historyId) {
      set({
        chatHistories: histories,
        messages: [],
        sessionId: generateSessionId(),
        isCentered: true,
      });
    } else {
      set({ chatHistories: histories });
    }
  },

  setCentered(centered: boolean) {
    set({ isCentered: centered });
  },

  setShowLoading(show: boolean) {
    set({ showLoading: show });
  },
}));

export default useChatStore;
