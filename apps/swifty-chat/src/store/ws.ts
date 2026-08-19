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
import { WS_URL } from "../config";

export interface WsState {
  status: "disconnected" | "connecting" | "connected";
  onMessageHandler: ((msg: MessageEvent) => void) | null;
  connect: (uuid: string) => void;
  disconnect: () => void;
  send: (data: unknown) => void;
  setOnMessage: (handler: ((msg: MessageEvent) => void) | null) => void;
}

let rawSocket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectDelay = 1000;
let intentionalClose = false;

const MAX_RECONNECT_DELAY = 30_000;

function scheduleReconnect(uuid: string) {
  if (intentionalClose) return;
  reconnectTimer = setTimeout(() => {
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
    doConnect(uuid);
  }, reconnectDelay);
}

function doConnect(uuid: string) {
  if (rawSocket) {
    rawSocket.onclose = null;
    rawSocket.close();
  }
  useWsStore.setState({ status: "connecting" });
  const ws = new WebSocket(WS_URL + "/wss?client_id=" + uuid);
  ws.onopen = () => {
    reconnectDelay = 1000;
    useWsStore.setState({ status: "connected" });
  };
  ws.onmessage = (msg: MessageEvent) => {
    const handler = useWsStore.getState().onMessageHandler;
    if (handler) handler(msg);
  };
  ws.onclose = () => {
    rawSocket = null;
    useWsStore.setState({ status: "disconnected" });
    scheduleReconnect(uuid);
  };
  ws.onerror = () => {
    ws.close();
  };
  rawSocket = ws;
}

const useWsStore = create<WsState>((set) => ({
  status: "disconnected",
  onMessageHandler: null,

  connect(uuid: string) {
    intentionalClose = false;
    reconnectDelay = 1000;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    doConnect(uuid);
  },

  disconnect() {
    intentionalClose = true;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (rawSocket) {
      rawSocket.onclose = null;
      rawSocket.close();
      rawSocket = null;
    }
    set({ status: "disconnected" });
  },

  send(data: unknown) {
    if (rawSocket && rawSocket.readyState === WebSocket.OPEN) {
      rawSocket.send(JSON.stringify(data));
    }
  },

  setOnMessage(handler: ((msg: MessageEvent) => void) | null) {
    set({ onMessageHandler: handler });
  },
}));

export default useWsStore;
