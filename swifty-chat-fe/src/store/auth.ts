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
import type { UserInfo } from "../types";
import { resolveAvatar } from "../utils/avatar";

export interface AuthState {
  userInfo: UserInfo;
  isLoggedIn: boolean;
  setUserInfo: (info: UserInfo) => void;
  clearUserInfo: () => void;
}

const emptyUser: UserInfo = {
  uuid: "",
  nickname: "",
  telephone: "",
  email: "",
  avatar: "",
  gender: 0,
  birthday: "",
  signature: "",
  status: 0,
  is_admin: 0,
  created_at: "",
};

function loadUserInfo(): UserInfo {
  try {
    const raw = sessionStorage.getItem("userInfo");
    return raw ? JSON.parse(raw) : { ...emptyUser };
  } catch {
    return { ...emptyUser };
  }
}

const initialUser = loadUserInfo();

const useAuthStore = create<AuthState>((set) => ({
  userInfo: initialUser,
  isLoggedIn: !!initialUser.uuid,
  setUserInfo(info: UserInfo) {
    info.avatar = resolveAvatar(info.avatar);
    sessionStorage.setItem("userInfo", JSON.stringify(info));
    set({ userInfo: info, isLoggedIn: !!info.uuid });
  },
  clearUserInfo() {
    sessionStorage.removeItem("userInfo");
    set({ userInfo: { ...emptyUser }, isLoggedIn: false });
  },
}));

export default useAuthStore;
export { emptyUser };
