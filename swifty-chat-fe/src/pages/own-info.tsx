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

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Card } from "@/components/ui/card";
import { NavBar } from "@/components/nav-bar";
import { ContactSidebar } from "@/components/contact-sidebar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import useAuthStore from "@/store/auth";
import { api } from "@/service/api";
import { showToast } from "@/utils/toast";
import { isValidEmail } from "@/utils/validate";
import { performLogout } from "@/utils/logout";
import type { UserInfo } from "@/types";

export default function OwnInfo() {
  const navigate = useNavigate();
  const userInfo = useAuthStore((s) => s.userInfo);

  const [editOpen, setEditOpen] = useState(false);
  const [editNick, setEditNick] = useState("");
  const [editEmail, setEditEmail] = useState("");
  const [editBirthday, setEditBirthday] = useState("");
  const [editSig, setEditSig] = useState("");
  const [avatarFile, setAvatarFile] = useState<File | null>(null);

  const handleLogout = async () => {
    await performLogout();
    navigate("/login");
  };

  const closeEditModal = () => {
    setEditOpen(false);
    setEditNick("");
    setEditEmail("");
    setEditBirthday("");
    setEditSig("");
    setAvatarFile(null);
  };

  const saveProfile = async () => {
    if (!editNick && !editEmail && !editBirthday && !editSig && !avatarFile) {
      showToast("Please modify at least one field", "warning");
      return;
    }
    if (editNick && (editNick.length < 3 || editNick.length > 10)) {
      showToast("Nickname must be 3-10 characters", "error");
      return;
    }
    if (editEmail && !isValidEmail(editEmail)) {
      showToast("Invalid email address", "error");
      return;
    }
    const data: Record<string, unknown> = { uuid: userInfo.uuid };
    if (editNick) data.nickname = editNick;
    if (editEmail) data.email = editEmail;
    if (editBirthday) data.birthday = editBirthday;
    if (editSig) data.signature = editSig;
    let avatarUrl = "";
    if (avatarFile) {
      const formData = new FormData();
      formData.append("file", avatarFile);
      const uploadRes = await api.uploadAvatar(formData);
      avatarUrl = (uploadRes.data as { url?: string } | null)?.url ?? "";
      if (uploadRes.code !== 200 || !avatarUrl) {
        showToast(uploadRes.message || "Avatar upload failed", "error");
        return;
      }
      data.avatar = avatarUrl;
    }
    const res = await api.updateUserInfo(data);
    if (res.code === 200) {
      showToast(res.message, "success");
      const updated: UserInfo = { ...userInfo };
      if (editNick) updated.nickname = editNick;
      if (editEmail) updated.email = editEmail;
      if (editBirthday) updated.birthday = editBirthday;
      if (editSig) updated.signature = editSig;
      if (avatarUrl) updated.avatar = avatarUrl;
      useAuthStore.getState().setUserInfo(updated);
      closeEditModal();
    } else {
      showToast(res.message, "error");
    }
  };

  return (
    <div className="bg-background flex min-h-screen items-center justify-center p-4">
      <Card className="shadow-primary/5 h-[600px] w-[1000px] flex-row shadow-xl">
        <NavBar
          avatar={userInfo.avatar}
          isAdmin={userInfo.is_admin === 1}
          onNavigate={(path) => navigate(path)}
          onLogout={handleLogout}
        />
        <div className="border-border w-55 border-r">
          <ContactSidebar onNavigate={(id) => navigate(`/chat/${id}`)} />
        </div>
        <div className="relative flex flex-1 flex-col items-center justify-center p-8">
          <h2 className="mb-6 text-xl font-semibold">My Profile</h2>
          <div className="text-foreground flex flex-col gap-2 text-sm">
            <p>
              <span className="text-muted-foreground">User ID:</span>{" "}
              {userInfo.uuid}
            </p>
            <p>
              <span className="text-muted-foreground">Nickname:</span>{" "}
              {userInfo.nickname}
            </p>
            <p>
              <span className="text-muted-foreground">Phone:</span>{" "}
              {userInfo.telephone}
            </p>
            <p>
              <span className="text-muted-foreground">Email:</span>{" "}
              {userInfo.email}
            </p>
            <p>
              <span className="text-muted-foreground">Gender:</span>{" "}
              {userInfo.gender === 0 ? "Male" : "Female"}
            </p>
            <p>
              <span className="text-muted-foreground">Birthday:</span>{" "}
              {userInfo.birthday}
            </p>
            <p>
              <span className="text-muted-foreground">Signature:</span>{" "}
              {userInfo.signature}
            </p>
            <p>
              <span className="text-muted-foreground">Joined:</span>{" "}
              {userInfo.created_at}
            </p>
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Avatar:</span>
              <Avatar className="ring-primary/30 ring-offset-card size-10 ring-2 ring-offset-2">
                <AvatarImage
                  src={userInfo.avatar || undefined}
                  alt={userInfo.nickname}
                />
                <AvatarFallback>
                  {userInfo.nickname.charAt(0).toUpperCase() || "?"}
                </AvatarFallback>
              </Avatar>
            </div>
          </div>
          <Button
            size="sm"
            className="absolute right-6 bottom-6"
            onClick={() => setEditOpen(true)}
          >
            Edit
          </Button>

          <Dialog
            open={editOpen}
            onOpenChange={(open) => {
              if (!open) closeEditModal();
            }}
          >
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>Edit Profile</DialogTitle>
              </DialogHeader>
              <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="edit-nickname">Nickname</Label>
                  <Input
                    id="edit-nickname"
                    type="text"
                    placeholder="Optional, 3-10 characters"
                    value={editNick}
                    onChange={(e) => setEditNick(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="edit-email">Email</Label>
                  <Input
                    id="edit-email"
                    type="text"
                    placeholder="Optional"
                    value={editEmail}
                    onChange={(e) => setEditEmail(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="edit-birthday">Birthday</Label>
                  <Input
                    id="edit-birthday"
                    type="text"
                    placeholder="Optional, e.g. 2024.1.1"
                    value={editBirthday}
                    onChange={(e) => setEditBirthday(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="edit-signature">Signature</Label>
                  <Input
                    id="edit-signature"
                    type="text"
                    placeholder="Optional"
                    value={editSig}
                    onChange={(e) => setEditSig(e.target.value)}
                  />
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="edit-avatar">Avatar</Label>
                  <Input
                    id="edit-avatar"
                    type="file"
                    onChange={(e) => setAvatarFile(e.target.files?.[0] ?? null)}
                  />
                </div>
              </div>
              <DialogFooter>
                <Button size="sm" onClick={saveProfile}>
                  Save
                </Button>
                <Button variant="ghost" size="sm" onClick={closeEditModal}>
                  Cancel
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      </Card>
    </div>
  );
}
