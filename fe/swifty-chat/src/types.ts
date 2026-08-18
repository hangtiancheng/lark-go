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

export interface UserInfo {
  uuid: string;
  nickname: string;
  telephone: string;
  email: string;
  avatar: string;
  gender: number;
  birthday: string;
  signature: string;
  status: number;
  is_admin: number;
  created_at: string;
}

export interface ContactInfo {
  contact_id: string;
  contact_name: string;
  contact_avatar: string;
  contact_phone: string;
  contact_email: string;
  contact_gender: number;
  contact_signature: string;
  contact_birthday: string;
  contact_notice: string;
  contact_members: string[];
  contact_member_cnt: number;
  contact_owner_id: string;
  contact_add_mode: number;
}

export interface Message {
  uuid?: string;
  session_id: string;
  type: number;
  content: string;
  url: string;
  send_id: string;
  send_name: string;
  send_avatar: string;
  receive_id: string;
  file_size: string;
  file_name: string;
  file_type: string;
  created_at: string;
  av_data?: string;
}

export interface SessionItem {
  user_id?: string;
  user_name?: string;
  group_id?: string;
  group_name?: string;
  avatar: string;
}

export interface ApiResponse<T = unknown> {
  code: number;
  message: string;
  data: T;
}
