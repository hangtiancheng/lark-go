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
import type { ContactInfo, Message } from "../types";

export interface ChatState {
  contactInfo: ContactInfo | null;
  sessionId: string;
  messageList: Message[];
  setContact: (info: ContactInfo) => void;
  setSessionId: (id: string) => void;
  addMessage: (msg: Message) => void;
  setMessageList: (list: Message[]) => void;
  clearChat: () => void;
}

const useChatStore = create<ChatState>((set, get) => ({
  contactInfo: null as ContactInfo | null,
  sessionId: "",
  messageList: [] as Message[],

  setContact(info: ContactInfo) {
    set({ contactInfo: info });
  },
  setSessionId(id: string) {
    set({ sessionId: id });
  },
  addMessage(msg: Message) {
    set({ messageList: [...get().messageList, msg] });
  },
  setMessageList(list: Message[]) {
    set({ messageList: list || [] });
  },
  clearChat() {
    set({ contactInfo: null, sessionId: "", messageList: [] });
  },
}));

export default useChatStore;
