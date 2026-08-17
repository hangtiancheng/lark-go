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

import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { MoreVertical, Paperclip, Video } from "lucide-react";
import { NavBar } from "@/components/nav-bar";
import { SessionSidebar } from "@/components/session-sidebar";
import { MessageBubble } from "@/components/message-bubble";
import { VideoCall, type VideoCallHandle } from "@/components/video-call";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { api } from "@/service/api";
import useAuthStore from "@/store/auth";
import useChatStore from "@/store/chat";
import useWsStore from "@/store/ws";
import { resolveAvatar } from "@/utils/avatar";
import { showToast } from "@/utils/toast";
import { performLogout } from "@/utils/logout";
import { getFileSize } from "@/utils/format";
import { BASE_URL } from "@/config";
import type { ContactInfo, Message } from "@/types";

interface MemberRow {
  user_id: string;
  nickname: string;
  avatar: string;
}

interface JoinRequestRow {
  apply_id: string;
  user_id: string;
  contact_id: string;
  contact_name: string;
  contact_type: number;
  status: number;
  message: string;
}

export default function Chat() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const userInfo = useAuthStore((s) => s.userInfo);
  const contactInfo = useChatStore((s) => s.contactInfo);
  const messageList = useChatStore((s) => s.messageList);

  const [chatMessage, setChatMessage] = useState("");
  const [memberList, setMemberList] = useState<MemberRow[]>([]);
  const [joinRequestList, setJoinRequestList] = useState<JoinRequestRow[]>([]);
  const [selectedMembers, setSelectedMembers] = useState<string[]>([]);

  // Edit group modal buffers
  const [editGroupName, setEditGroupName] = useState("");
  const [editGroupNotice, setEditGroupNotice] = useState("");
  const [editGroupAddMode, setEditGroupAddMode] = useState(-1);
  const [groupAvatarFile, setGroupAvatarFile] = useState<File | null>(null);

  // Dialog open states
  const [userInfoOpen, setUserInfoOpen] = useState(false);
  const [groupInfoOpen, setGroupInfoOpen] = useState(false);
  const [editGroupOpen, setEditGroupOpen] = useState(false);
  const [removeMembersOpen, setRemoveMembersOpen] = useState(false);
  const [joinRequestsOpen, setJoinRequestsOpen] = useState(false);

  const videoCallRef = useRef<VideoCallHandle>(null);

  // Derived contact fields
  const contactId = contactInfo?.contact_id ?? "";
  const contactName = contactInfo?.contact_name ?? "";
  const contactAvatar = contactInfo?.contact_avatar ?? "";
  const isUserContact = contactId.startsWith("U");
  const isGroupContact = contactId !== "" && !isUserContact;
  const isGroupOwner = contactInfo?.contact_owner_id === userInfo.uuid;
  const contactGenderText = isUserContact
    ? contactInfo?.contact_gender === 0
      ? "Male"
      : "Female"
    : "";
  const contactPhone = contactInfo?.contact_phone ?? "";
  const contactEmail = contactInfo?.contact_email ?? "";
  const contactBirthday = contactInfo?.contact_birthday ?? "";
  const contactSignature = contactInfo?.contact_signature ?? "";
  const groupMemberCnt = isGroupContact
    ? (contactInfo?.contact_member_cnt ?? 0)
    : 0;
  const groupOwnerId = contactInfo?.contact_owner_id ?? "";
  const groupAddModeText = isGroupContact
    ? contactInfo?.contact_add_mode === 0
      ? "Direct Join"
      : "Owner Approval"
    : "";
  const groupNotice = contactInfo?.contact_notice ?? "";

  const scrollToBottom = () => {
    const el = document.getElementById("chat-messages");
    if (el) el.scrollTop = el.scrollHeight;
  };

  const loadMessages = async (cid: string) => {
    const isUser = cid.startsWith("U");
    const res = isUser
      ? await api.getMessageList({
          send_id: userInfo.uuid,
          receive_id: cid,
        })
      : await api.getGroupMessageList({ group_id: cid });
    if (res.code === 200 && res.data) {
      const list = ((res.data as Message[]) || []).map((m) => ({
        ...m,
        send_avatar: resolveAvatar(m.send_avatar),
      }));
      useChatStore.getState().setMessageList(list);
    }
  };

  const loadChat = async (cid: string) => {
    const res = await api.getContactInfo({
      user_id: userInfo.uuid,
      contact_id: cid,
    });
    if (res.code !== 200 || !res.data) return;
    const info = res.data as ContactInfo;
    info.contact_avatar = resolveAvatar(info.contact_avatar);
    useChatStore.getState().setContact(info);

    const sessionRes = await api.openSession({
      send_id: userInfo.uuid,
      receive_id: cid,
    });
    if (sessionRes.code === 200) {
      useChatStore.getState().setSessionId(sessionRes.data as string);
      await loadMessages(cid);
    }
  };

  // Load chat on id change
  useEffect(() => {
    useChatStore.getState().clearChat();
    if (id) {
      loadChat(id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  // Auto scroll to bottom on new messages
  useEffect(() => {
    scrollToBottom();
  }, [messageList]);

  // Subscribe to websocket messages
  useEffect(() => {
    const handleWsMessage = (event: MessageEvent) => {
      const raw = event.data;
      if (typeof raw !== "string" || raw.length === 0 || raw.charAt(0) !== "{")
        return;
      let message: Message;
      try {
        message = JSON.parse(raw) as Message;
      } catch {
        return;
      }
      const auth = useAuthStore.getState();
      const chat = useChatStore.getState();

      if (message.type === 3) {
        try {
          const avData = JSON.parse(message.av_data || "{}") as Record<
            string,
            unknown
          >;
          videoCallRef.current?.handleSignal(avData);
        } catch {
          /* ignore malformed signal */
        }
        return;
      }

      const currentContactId = chat.contactInfo?.contact_id;
      const isRelevant =
        (message.receive_id.startsWith("G") &&
          message.receive_id === currentContactId) ||
        (message.receive_id.startsWith("U") &&
          message.receive_id === auth.userInfo.uuid &&
          message.send_id === currentContactId) ||
        (message.send_id === auth.userInfo.uuid &&
          message.receive_id === currentContactId);

      if (isRelevant) {
        message.send_avatar = resolveAvatar(message.send_avatar);
        useChatStore.getState().addMessage(message);
      }
    };
    useWsStore.getState().setOnMessage(handleWsMessage);
    return () => {
      useWsStore.getState().setOnMessage(() => {});
    };
  }, []);

  const handleLogout = async () => {
    await performLogout();
    navigate("/login");
  };

  const sendMessage = () => {
    if (!chatMessage.trim() || !contactInfo) return;
    const msg: Message = {
      session_id: useChatStore.getState().sessionId,
      type: 0,
      content: chatMessage,
      url: "",
      send_id: userInfo.uuid,
      send_name: userInfo.nickname,
      send_avatar: userInfo.avatar,
      receive_id: contactInfo.contact_id,
      file_size: getFileSize(0),
      file_name: "",
      file_type: "",
      created_at: new Date().toISOString(),
    };
    useWsStore.getState().send(msg);
    setChatMessage("");
  };

  const onFileSelect = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const formData = new FormData();
    formData.append("file", file);
    const res = await api.uploadFile(formData);
    const data = res.data as { url?: string; file_name?: string } | null;
    const uploadedUrl = data?.url || "";
    const uploadedName = data?.file_name || file.name;
    if (!uploadedUrl) {
      showToast("File upload failed", "error");
      return;
    }
    showToast("File uploaded successfully", "success");
    if (!contactInfo) return;
    const msg: Message = {
      session_id: useChatStore.getState().sessionId,
      type: 2,
      content: "",
      url: BASE_URL + uploadedUrl,
      send_id: userInfo.uuid,
      send_name: userInfo.nickname,
      send_avatar: userInfo.avatar,
      receive_id: contactInfo.contact_id,
      file_size: getFileSize(file.size),
      file_name: uploadedName,
      file_type: file.type,
      created_at: new Date().toISOString(),
    };
    useWsStore.getState().send(msg);
    e.target.value = "";
  };

  const openVideoCall = () => {
    videoCallRef.current?.show();
  };

  const deleteSession = async () => {
    const sessionId = useChatStore.getState().sessionId;
    await api.deleteSession({ owner_id: userInfo.uuid, session_id: sessionId });
    navigate("/chat/sessions");
  };

  const deleteContact = async () => {
    if (!contactInfo) return;
    await api.deleteContact({
      user_id: userInfo.uuid,
      contact_id: contactInfo.contact_id,
    });
    showToast("Contact removed", "success");
    navigate("/chat/sessions");
  };

  const blackContact = async () => {
    if (!contactInfo) return;
    await api.blackContact({
      user_id: userInfo.uuid,
      contact_id: contactInfo.contact_id,
    });
    showToast("Contact blocked", "success");
    navigate("/chat/sessions");
  };

  const dismissGroup = async () => {
    if (!contactInfo) return;
    const res = await api.dismissGroup({ group_id: contactInfo.contact_id });
    if (res.code === 200) {
      showToast("Group disbanded", "success");
      navigate("/chat/sessions");
    } else {
      showToast(res.message, "error");
    }
  };

  const leaveGroup = async () => {
    if (!contactInfo) return;
    const res = await api.leaveGroup({
      user_id: userInfo.uuid,
      group_id: contactInfo.contact_id,
    });
    if (res.code === 200) {
      showToast("Left group", "success");
      navigate("/chat/sessions");
    } else {
      showToast(res.message, "error");
    }
  };

  const showEditGroupModal = () => {
    setEditGroupName("");
    setEditGroupNotice("");
    setEditGroupAddMode(-1);
    setGroupAvatarFile(null);
    setEditGroupOpen(true);
  };

  const saveGroupInfo = async () => {
    if (
      !editGroupName &&
      !editGroupNotice &&
      editGroupAddMode === -1 &&
      !groupAvatarFile
    ) {
      showToast("Please modify at least one field", "warning");
      return;
    }
    if (
      editGroupName &&
      (editGroupName.length < 3 || editGroupName.length > 10)
    ) {
      showToast("Group name must be 3-10 characters", "error");
      return;
    }
    if (!contactInfo) return;
    let avatarUrl = "";
    if (groupAvatarFile) {
      const formData = new FormData();
      formData.append("file", groupAvatarFile);
      const uploadRes = await api.uploadAvatar(formData);
      avatarUrl = (uploadRes.data as { url?: string } | null)?.url ?? "";
      if (uploadRes.code !== 200 || !avatarUrl) {
        showToast(uploadRes.message || "Avatar upload failed", "error");
        return;
      }
    }
    const data: Record<string, unknown> = { uuid: contactInfo.contact_id };
    if (editGroupName) data.name = editGroupName;
    if (editGroupNotice) data.notice = editGroupNotice;
    if (editGroupAddMode !== -1) data.add_mode = editGroupAddMode;
    if (avatarUrl) data.avatar = avatarUrl;
    const res = await api.updateGroupInfo(data);
    if (res.code === 200) {
      showToast("Group updated", "success");
      setEditGroupOpen(false);
      loadChat(contactInfo.contact_id);
    } else {
      showToast(res.message, "error");
    }
  };

  const showRemoveMembersModal = async () => {
    if (!contactInfo) return;
    setSelectedMembers([]);
    const res = await api.getGroupMemberList({
      group_id: contactInfo.contact_id,
    });
    const list = ((res.data as MemberRow[]) || []).map((m) => ({
      ...m,
      avatar: resolveAvatar(m.avatar),
    }));
    setMemberList(list);
    setRemoveMembersOpen(true);
  };

  const toggleMember = (mid: string, checked: boolean) => {
    setSelectedMembers((prev) =>
      checked ? [...prev, mid] : prev.filter((x) => x !== mid),
    );
  };

  const removeSelectedMembers = async () => {
    if (selectedMembers.length === 0) {
      showToast("Please select members to remove", "warning");
      return;
    }
    if (!contactInfo) return;
    const res = await api.removeGroupMembers({
      group_id: contactInfo.contact_id,
      member_ids: selectedMembers,
    });
    if (res.code === 200) {
      showToast("Members removed", "success");
      const remaining = memberList.filter(
        (m) => !selectedMembers.includes(m.user_id),
      );
      setMemberList(remaining);
      setSelectedMembers([]);
    } else {
      showToast(res.message, "error");
    }
  };

  const showJoinRequestsModal = async () => {
    const res = await api.getAddGroupList({ user_id: userInfo.uuid });
    const list = ((res.data as JoinRequestRow[]) || []).filter(
      (r) => r.contact_type === 1 && r.status === 0,
    );
    if (list.length === 0) {
      showToast("No pending join requests", "info");
      return;
    }
    setJoinRequestList(list);
    setJoinRequestsOpen(true);
  };

  const approveJoinRequest = async (applyId: string) => {
    const res = await api.passContactApply({ apply_id: applyId });
    if (res.code === 200) {
      showToast("Approved", "success");
      setJoinRequestList((prev) => prev.filter((r) => r.apply_id !== applyId));
    } else {
      showToast(res.message, "error");
    }
  };

  const rejectJoinRequest = async (applyId: string) => {
    const res = await api.refuseContactApply({ apply_id: applyId });
    if (res.code === 200) {
      showToast("Rejected", "success");
      setJoinRequestList((prev) => prev.filter((r) => r.apply_id !== applyId));
    } else {
      showToast(res.message, "error");
    }
  };

  const infoRow = (label: string, value: string | number) => (
    <div className="border-border flex justify-between border-b py-1.5">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-foreground">{value}</span>
    </div>
  );

  return (
    <div className="bg-background flex min-h-screen items-center justify-center p-4">
      <Card className="shadow-primary/5 h-[600px] w-[1000px] flex-row gap-0 p-0 shadow-xl">
        <NavBar
          avatar={userInfo.avatar}
          isAdmin={userInfo.is_admin === 1}
          onNavigate={(path) => navigate(path)}
          onLogout={handleLogout}
        />
        <div className="border-border w-55 border-r">
          <SessionSidebar onChat={(cid) => navigate(`/chat/${cid}`)} />
        </div>
        <div className="flex flex-1 flex-col">
          <div className="border-border bg-muted/30 flex h-14 items-center justify-between border-b px-4">
            <div className="flex items-center gap-3">
              {contactAvatar && (
                <Avatar className="ring-border ring-offset-card size-10 ring-2 ring-offset-2">
                  <AvatarImage src={contactAvatar} alt={contactName} />
                  <AvatarFallback>
                    {contactName.charAt(0) || "?"}
                  </AvatarFallback>
                </Avatar>
              )}
              <h2 className="text-foreground text-base font-semibold">
                {contactName}
              </h2>
            </div>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    className="text-muted-foreground"
                    aria-label="Chat options"
                  />
                }
              >
                <MoreVertical className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                {isUserContact && (
                  <DropdownMenuItem
                    className="text-sm"
                    onClick={() => setUserInfoOpen(true)}
                  >
                    User Info
                  </DropdownMenuItem>
                )}
                {isGroupContact && (
                  <DropdownMenuItem
                    className="text-sm"
                    onClick={() => setGroupInfoOpen(true)}
                  >
                    Group Info
                  </DropdownMenuItem>
                )}
                {isGroupContact && isGroupOwner && (
                  <>
                    <DropdownMenuItem
                      className="text-sm"
                      onClick={showEditGroupModal}
                    >
                      Edit Group
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      className="text-sm"
                      onClick={showRemoveMembersModal}
                    >
                      Remove Members
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      className="text-sm"
                      onClick={showJoinRequestsModal}
                    >
                      Join Requests
                    </DropdownMenuItem>
                  </>
                )}
                <DropdownMenuItem className="text-sm" onClick={deleteSession}>
                  Delete Session
                </DropdownMenuItem>
                {isUserContact && (
                  <>
                    <DropdownMenuItem
                      className="text-sm"
                      onClick={deleteContact}
                    >
                      Remove Contact
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      className="text-destructive focus:text-destructive text-sm"
                      onClick={blackContact}
                    >
                      Block Contact
                    </DropdownMenuItem>
                  </>
                )}
                {isGroupContact && isGroupOwner && (
                  <DropdownMenuItem
                    className="text-destructive focus:text-destructive text-sm"
                    onClick={dismissGroup}
                  >
                    Disband Group
                  </DropdownMenuItem>
                )}
                {isGroupContact && !isGroupOwner && (
                  <DropdownMenuItem className="text-sm" onClick={leaveGroup}>
                    Leave Group
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          <div
            className="bg-muted/20 flex-1 overflow-y-auto p-4"
            id="chat-messages"
          >
            <MessageBubble
              messageList={messageList}
              currentUserId={userInfo.uuid}
              currentUserAvatar={userInfo.avatar}
              currentUserName={userInfo.nickname}
            />
          </div>

          <div className="border-border bg-muted/30 flex h-10 items-center justify-between gap-1 border-t px-2">
            <div className="flex items-center gap-1">
              <label className="cursor-pointer">
                <input type="file" className="hidden" onChange={onFileSelect} />
                <span className="text-muted-foreground hover:bg-accent hover:text-accent-foreground flex size-8 items-center justify-center rounded-md transition-all duration-200">
                  <Paperclip size={16} />
                </span>
              </label>
            </div>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    className="text-muted-foreground hover:bg-accent"
                    onClick={openVideoCall}
                    aria-label="Video call"
                  />
                }
              >
                <Video size={16} />
              </TooltipTrigger>
              <TooltipContent side="left">Video Call</TooltipContent>
            </Tooltip>
          </div>

          <VideoCall ref={videoCallRef} />

          <div className="border-border flex h-[180px] border-t">
            <Textarea
              className="bg-card placeholder:text-muted-foreground/50 flex-1 resize-none rounded-none border-0 p-3 text-sm focus-visible:ring-0"
              placeholder="Type a message..."
              maxLength={500}
              value={chatMessage}
              onChange={(e) => setChatMessage(e.target.value)}
            />
            <div className="flex w-[68px] flex-col-reverse p-2">
              <Button className="h-10" onClick={sendMessage}>
                Send
              </Button>
            </div>
          </div>
        </div>

        {/* User Info Dialog */}
        <Dialog open={userInfoOpen} onOpenChange={setUserInfoOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>User Profile</DialogTitle>
            </DialogHeader>
            <div className="flex flex-col gap-0.5 text-sm">
              {infoRow("ID", contactId)}
              {infoRow("Name", contactName)}
              {infoRow("Gender", contactGenderText)}
              {infoRow("Phone", contactPhone)}
              {infoRow("Email", contactEmail)}
              {infoRow("Birthday", contactBirthday)}
              <div className="py-1.5">
                <span className="text-muted-foreground">Signature</span>
                <p className="text-foreground mt-1">{contactSignature}</p>
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setUserInfoOpen(false)}
              >
                Close
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Group Info Dialog */}
        <Dialog open={groupInfoOpen} onOpenChange={setGroupInfoOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Group Info</DialogTitle>
            </DialogHeader>
            <div className="flex flex-col gap-0.5 text-sm">
              {infoRow("ID", contactId)}
              {infoRow("Name", contactName)}
              {infoRow("Members", groupMemberCnt)}
              {infoRow("Owner", groupOwnerId)}
              {infoRow("Join Mode", groupAddModeText)}
              <div className="py-1.5">
                <span className="text-muted-foreground">Notice</span>
                <p className="text-foreground mt-1">{groupNotice}</p>
              </div>
            </div>
            <DialogFooter>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setGroupInfoOpen(false)}
              >
                Close
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Edit Group Dialog */}
        <Dialog open={editGroupOpen} onOpenChange={setEditGroupOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Edit Group</DialogTitle>
            </DialogHeader>
            <div className="flex flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="edit-group-name">Group Name</Label>
                <Input
                  id="edit-group-name"
                  type="text"
                  placeholder="3-10 characters"
                  value={editGroupName}
                  onChange={(e) => setEditGroupName(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="edit-group-notice">Notice</Label>
                <Textarea
                  id="edit-group-notice"
                  rows={3}
                  placeholder="Optional"
                  maxLength={500}
                  value={editGroupNotice}
                  onChange={(e) => setEditGroupNotice(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label>Join Mode</Label>
                <RadioGroup
                  value={
                    editGroupAddMode === -1
                      ? undefined
                      : String(editGroupAddMode)
                  }
                  onValueChange={(v) => setEditGroupAddMode(Number(v))}
                  className="flex gap-4"
                >
                  <div className="flex items-center gap-2">
                    <RadioGroupItem value="0" id="addmode-0" />
                    <Label htmlFor="addmode-0" className="font-normal">
                      Direct Join
                    </Label>
                  </div>
                  <div className="flex items-center gap-2">
                    <RadioGroupItem value="1" id="addmode-1" />
                    <Label htmlFor="addmode-1" className="font-normal">
                      Owner Approval
                    </Label>
                  </div>
                </RadioGroup>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="edit-group-avatar">Avatar</Label>
                <Input
                  id="edit-group-avatar"
                  type="file"
                  accept="image/*"
                  onChange={(e) =>
                    setGroupAvatarFile(e.target.files?.[0] ?? null)
                  }
                />
              </div>
            </div>
            <DialogFooter>
              <Button size="sm" onClick={saveGroupInfo}>
                Save
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setEditGroupOpen(false)}
              >
                Cancel
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Remove Members Dialog */}
        <Dialog open={removeMembersOpen} onOpenChange={setRemoveMembersOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Remove Group Members</DialogTitle>
            </DialogHeader>
            {memberList.length === 0 && (
              <p className="text-muted-foreground py-4 text-center text-sm">
                No members found
              </p>
            )}
            <div className="flex max-h-60 flex-col overflow-y-auto">
              {memberList.map((m) => (
                <div
                  key={m.user_id}
                  className="border-border hover:bg-accent/50 flex cursor-pointer items-center justify-between rounded-md border-b px-2 py-2 transition-colors"
                  onClick={() =>
                    toggleMember(
                      m.user_id,
                      !selectedMembers.includes(m.user_id),
                    )
                  }
                >
                  <div className="flex items-center gap-2">
                    <Avatar>
                      <AvatarImage src={m.avatar} alt={m.nickname} />
                      <AvatarFallback>
                        {(m.nickname || "?").charAt(0)}
                      </AvatarFallback>
                    </Avatar>
                    <span className="text-foreground text-sm">
                      {m.nickname}
                    </span>
                  </div>
                  <Checkbox
                    checked={selectedMembers.includes(m.user_id)}
                    onCheckedChange={(checked) =>
                      toggleMember(m.user_id, checked === true)
                    }
                  />
                </div>
              ))}
            </div>
            <DialogFooter>
              <Button
                variant="destructive"
                size="sm"
                onClick={removeSelectedMembers}
              >
                Remove Selected
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setRemoveMembersOpen(false)}
              >
                Cancel
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Join Requests Dialog */}
        <Dialog open={joinRequestsOpen} onOpenChange={setJoinRequestsOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Group Join Requests</DialogTitle>
            </DialogHeader>
            {joinRequestList.length === 0 && (
              <p className="text-muted-foreground py-4 text-center text-sm">
                No pending requests
              </p>
            )}
            <div className="flex max-h-60 flex-col gap-2 overflow-y-auto">
              {joinRequestList.map((req) => (
                <div
                  key={req.apply_id}
                  className="border-border flex items-center justify-between border-b py-2"
                >
                  <div className="flex items-center gap-2">
                    <span className="text-foreground text-sm">
                      {req.contact_name}
                    </span>
                    {req.message && (
                      <span className="text-muted-foreground text-xs">
                        ({req.message})
                      </span>
                    )}
                  </div>
                  <div className="flex gap-1">
                    <Button
                      size="sm"
                      onClick={() => approveJoinRequest(req.apply_id)}
                    >
                      Approve
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-muted-foreground"
                      onClick={() => rejectJoinRequest(req.apply_id)}
                    >
                      Reject
                    </Button>
                  </div>
                </div>
              ))}
            </div>
            <DialogFooter>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setJoinRequestsOpen(false)}
              >
                Close
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </Card>
    </div>
  );
}
