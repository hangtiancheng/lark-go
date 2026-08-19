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

import { create } from "zustand";

export interface EntrySnapshot {
  key: string;
  size: number;
  expire_at: number;
  level: number;
}

export interface GroupSnapshot {
  name: string;
  stats: Record<string, unknown>;
  entries: EntrySnapshot[];
}

export interface DashboardState {
  groups: GroupSnapshot[];
  status: string;
  ws: WebSocket | null;
  connect: (url: string) => void;
  disconnect: () => void;
  deleteKey: (group: string, key: string) => void;
}

const useDashboardStore = create<DashboardState>((set, get) => ({
  groups: [] as GroupSnapshot[],
  status: "disconnected",
  ws: null as WebSocket | null,
  _retryTimer: 0,
  _intentionalClose: false,

  connect(url: string) {
    const state = get() as DashboardState & {
      _retryTimer: number;
      _intentionalClose: boolean;
    };
    state._intentionalClose = false;
    if (state.ws) state.ws.close();
    if (state._retryTimer) clearTimeout(state._retryTimer);
    set({ status: "connecting" });

    const ws = new WebSocket(url);
    ws.onopen = () => {
      set({ status: "connected" });
    };
    ws.onmessage = (e: MessageEvent) => {
      const data = JSON.parse(e.data);
      if (data.type === "snapshot") {
        set({ groups: data.groups ?? [] });
      }
    };
    ws.onclose = () => {
      const s = get() as DashboardState & {
        _retryTimer: number;
        _intentionalClose: boolean;
      };
      set({ status: "disconnected", ws: null });
      if (!s._intentionalClose) {
        s._retryTimer = window.setTimeout(() => s.connect(url), 3000);
      }
    };
    ws.onerror = () => {
      set({ status: "disconnected" });
    };
    set({ ws } as unknown as Partial<DashboardState>);
  },

  disconnect() {
    const state = get() as DashboardState & {
      _retryTimer: number;
      _intentionalClose: boolean;
    };
    state._intentionalClose = true;
    if (state._retryTimer) clearTimeout(state._retryTimer);
    if (state.ws) state.ws.close();
    set({
      ws: null,
      status: "disconnected",
      groups: [],
    });
  },

  deleteKey(group: string, key: string) {
    const state = get() as DashboardState & {
      _retryTimer: number;
      _intentionalClose: boolean;
    };
    if (state.ws && state.ws.readyState === WebSocket.OPEN) {
      state.ws.send(JSON.stringify({ action: "delete", group, key }));
    }
  },
}));

export default useDashboardStore;
