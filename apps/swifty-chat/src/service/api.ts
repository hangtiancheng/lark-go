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

import { BASE_URL } from "../config";
import type { ApiResponse } from "../types";

async function request<T>(
  endpoint: string,
  data?: unknown,
): Promise<ApiResponse<T>> {
  try {
    const response = await fetch(BASE_URL + endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data || {}),
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok) {
      return {
        code: response.status,
        message: `HTTP ${response.status}: ${response.statusText}`,
        data: null as T,
      };
    }
    return await response.json();
  } catch (err) {
    const message =
      err instanceof DOMException && err.name === "TimeoutError"
        ? "Request timed out"
        : "Network error";
    return { code: -1, message, data: null as T };
  }
}

async function upload(
  endpoint: string,
  formData: FormData,
): Promise<ApiResponse> {
  try {
    const response = await fetch(BASE_URL + endpoint, {
      method: "POST",
      body: formData,
      signal: AbortSignal.timeout(30_000),
    });
    if (!response.ok) {
      return {
        code: response.status,
        message: `HTTP ${response.status}: ${response.statusText}`,
        data: null,
      };
    }
    return await response.json();
  } catch {
    return { code: -1, message: "Upload failed", data: null };
  }
}

export const api = {
  // Auth
  login: (data: { telephone: string; password: string }) =>
    request("/login", data),
  register: (data: { nickname: string; telephone: string; password: string }) =>
    request("/register", data),

  // User
  getUserInfo: (data: { owner_id: string }) =>
    request("/user/get-user-info", data),
  getUserInfoList: (data: { owner_id: string }) =>
    request("/user/get-user-info-list", data),
  updateUserInfo: (data: unknown) => request("/user/update-user-info", data),
  wsLogout: (data: { owner_id: string }) => request("/user/ws-logout", data),
  ableUsers: (data: { uuid_list: string[] }) =>
    request("/user/able-users", data),
  disableUsers: (data: { uuid_list: string[] }) =>
    request("/user/disable-users", data),
  deleteUsers: (data: { uuid_list: string[] }) =>
    request("/user/delete-users", data),
  setAdmin: (data: { uuid_list: string[]; is_admin: number }) =>
    request("/user/set-admin", data),

  // Contact
  getContactInfo: (data: { user_id: string; contact_id: string }) =>
    request("/contact/get-contact-info", data),
  getUserList: (data: { owner_id: string }) =>
    request("/contact/get-user-list", data),
  applyContact: (data: {
    user_id: string;
    contact_id: string;
    contact_type: number;
    message: string;
  }) => request("/contact/apply-contact", data),
  passContactApply: (data: { apply_id: string }) =>
    request("/contact/pass-contact-apply", data),
  getNewContactList: (data: { user_id: string }) =>
    request("/contact/get-new-contact-list", data),
  refuseContactApply: (data: { apply_id: string }) =>
    request("/contact/refuse-contact-apply", data),
  blackApply: (data: { apply_id: string }) =>
    request("/contact/black-apply", data),
  getAddGroupList: (data: { user_id: string }) =>
    request("/contact/get-add-group-list", data),
  deleteContact: (data: { user_id: string; contact_id: string }) =>
    request("/contact/delete-contact", data),
  blackContact: (data: { user_id: string; contact_id: string }) =>
    request("/contact/black-contact", data),
  cancelBlackContact: (data: { user_id: string; contact_id: string }) =>
    request("/contact/cancel-black-contact", data),
  loadMyJoinedGroup: (data: { owner_id: string }) =>
    request("/contact/load-my-joined-group", data),

  // Session
  openSession: (data: { send_id: string; receive_id: string }) =>
    request("/session/open-session", data),
  getUserSessionList: (data: { owner_id: string }) =>
    request("/session/get-user-session-list", data),
  getGroupSessionList: (data: { owner_id: string }) =>
    request("/session/get-group-session-list", data),
  deleteSession: (data: { owner_id: string; session_id: string }) =>
    request("/session/delete-session", data),
  checkOpenSessionAllowed: (data: { send_id: string; receive_id: string }) =>
    request("/session/check-open-session-allowed", data),

  // Message
  getMessageList: (data: { send_id: string; receive_id: string }) =>
    request("/message/get-message-list", data),
  getGroupMessageList: (data: { group_id: string }) =>
    request("/message/get-group-message-list", data),
  uploadFile: (formData: FormData) => upload("/message/upload-file", formData),
  uploadAvatar: (formData: FormData) =>
    upload("/message/upload-avatar", formData),

  // Group
  createGroup: (data: {
    name: string;
    owner_id: string;
    avatar: string;
    notice?: string;
    add_mode?: number;
  }) => request("/group/create-group", data),
  loadMyGroup: (data: { owner_id: string }) =>
    request("/group/load-my-group", data),
  getGroupInfo: (data: { group_id: string }) =>
    request("/group/get-group-info", data),
  getGroupMemberList: (data: { group_id: string }) =>
    request("/group/get-group-member-list", data),
  updateGroupInfo: (data: unknown) => request("/group/update-group-info", data),
  removeGroupMembers: (data: { group_id: string; member_ids: string[] }) =>
    request("/group/remove-group-members", data),
  leaveGroup: (data: { user_id: string; group_id: string }) =>
    request("/group/leave-group", data),
  dismissGroup: (data: { group_id: string }) =>
    request("/group/dismiss-group", data),
  checkGroupAddMode: (data: { group_id: string }) =>
    request("/group/check-group-add-mode", data),
  enterGroupDirectly: (data: { user_id: string; group_id: string }) =>
    request("/group/enter-group-directly", data),

  // Group (admin)
  getGroupInfoList: (data: unknown) =>
    request("/group/get-group-info-list", data),
  deleteGroups: (data: { uuid_list: string[] }) =>
    request("/group/delete-groups", data),
  setGroupsStatus: (data: { uuid_list: string[]; status: number }) =>
    request("/group/set-groups-status", data),

  // Chatroom
  getOnlineUsers: () => request("/chatroom/get-online-users"),
};
