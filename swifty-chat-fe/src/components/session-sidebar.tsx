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

import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { api } from "@/service/api";
import useAuthStore from "@/store/auth";
import useSessionStore from "@/store/session";
import { cn } from "@/lib/utils";
import { resolveAvatar } from "@/utils/avatar";
import type { SessionItem } from "@/types";

interface SessionSidebarProps {
  onChat: (id: string) => void;
}

interface SessionSectionProps {
  title: string;
  open: boolean;
  loading: boolean;
  query: string;
  sessions: SessionItem[];
  rowId: (session: SessionItem) => string;
  rowName: (session: SessionItem) => string;
  onOpenChange: (open: boolean) => void;
  onChat: (id: string) => void;
}

function SessionSection({
  title,
  open,
  loading,
  query,
  sessions,
  rowId,
  rowName,
  onOpenChange,
  onChat,
}: SessionSectionProps) {
  const needle = query.trim().toLowerCase();
  const visible = needle
    ? sessions.filter((s) => rowName(s).toLowerCase().includes(needle))
    : sessions;

  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <CollapsibleTrigger
        className="border-border bg-muted/30 hover:bg-accent/50 flex w-full cursor-pointer items-center justify-between border-b px-3 py-2.5 transition-colors"
        aria-label={`${title} (${sessions.length})`}
      >
        <span className="text-foreground text-sm font-medium">
          {title}
          <span className="text-muted-foreground ml-2 text-xs font-normal">
            {sessions.length}
          </span>
        </span>
        <ChevronDown
          className={cn(
            "text-muted-foreground size-4 transition-transform duration-200",
            open && "rotate-180",
          )}
        />
      </CollapsibleTrigger>

      <CollapsibleContent className="data-open:animate-in data-open:fade-in-0 data-open:slide-in-from-top-1 overflow-hidden data-open:duration-200">
        {loading ? (
          <p className="text-muted-foreground animate-pulse px-3 py-3 text-xs">
            Loading sessions…
          </p>
        ) : visible.length === 0 ? (
          <p className="text-muted-foreground px-3 py-3 text-xs">
            {needle ? "No matches found" : "No sessions yet"}
          </p>
        ) : (
          visible.map((session) => {
            const id = rowId(session);
            const name = rowName(session);
            return (
              <button
                key={id || name}
                type="button"
                onClick={() => onChat(id)}
                className="hover:bg-accent/60 active:bg-accent flex w-full cursor-pointer items-center gap-2.5 px-3 py-2 text-left transition-colors duration-150"
              >
                <Avatar>
                  <AvatarImage src={session.avatar} alt={name} />
                  <AvatarFallback className="text-xs">
                    {(name.trim().charAt(0) || "?").toUpperCase()}
                  </AvatarFallback>
                </Avatar>
                <span className="text-foreground truncate text-sm">{name}</span>
              </button>
            );
          })
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function SessionSidebar({ onChat }: SessionSidebarProps) {
  const [query, setQuery] = useState("");
  const [usersOpen, setUsersOpen] = useState(true);
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [usersLoading, setUsersLoading] = useState(false);
  const [groupsLoading, setGroupsLoading] = useState(false);
  const groupsLoaded = useRef(false);

  const userSessions = useSessionStore((s) => s.userSessions);
  const groupSessions = useSessionStore((s) => s.groupSessions);

  const loadUserSessions = useCallback(async () => {
    const uid = useAuthStore.getState().userInfo.uuid;
    if (!uid) return;
    setUsersLoading(true);
    try {
      const res = await api.getUserSessionList({ owner_id: uid });
      if (res.code !== 200) return;
      const list = ((res.data as SessionItem[]) || []).map((u) => ({
        ...u,
        avatar: resolveAvatar(u.avatar),
      }));
      useSessionStore.getState().setUserSessions(list);
    } finally {
      setUsersLoading(false);
    }
  }, []);

  const loadGroupSessions = useCallback(async () => {
    const uid = useAuthStore.getState().userInfo.uuid;
    if (!uid) return;
    setGroupsLoading(true);
    try {
      const res = await api.getGroupSessionList({ owner_id: uid });
      if (res.code !== 200) return;
      const list = ((res.data as SessionItem[]) || []).map((g) => ({
        ...g,
        avatar: resolveAvatar(g.avatar),
      }));
      useSessionStore.getState().setGroupSessions(list);
    } finally {
      setGroupsLoading(false);
    }
  }, []);

  // Users section is open by default, so load it on mount.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadUserSessions();
  }, [loadUserSessions]);

  function handleGroupsOpenChange(open: boolean) {
    setGroupsOpen(open);
    if (open && !groupsLoaded.current) {
      groupsLoaded.current = true;
      void loadGroupSessions();
    }
  }

  return (
    <div className="flex h-full w-full flex-col">
      <div className="p-2">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search sessions"
          aria-label="Search sessions"
        />
      </div>

      <div className="flex-1 overflow-y-auto">
        <SessionSection
          title="Users"
          open={usersOpen}
          loading={usersLoading}
          query={query}
          sessions={userSessions}
          rowId={(u) => u.user_id ?? ""}
          rowName={(u) => u.user_name ?? ""}
          onOpenChange={setUsersOpen}
          onChat={onChat}
        />
        <SessionSection
          title="Groups"
          open={groupsOpen}
          loading={groupsLoading}
          query={query}
          sessions={groupSessions}
          rowId={(g) => g.group_id ?? ""}
          rowName={(g) => g.group_name ?? ""}
          onOpenChange={handleGroupsOpenChange}
          onChat={onChat}
        />
      </div>
    </div>
  );
}
