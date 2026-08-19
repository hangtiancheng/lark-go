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
import { Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api } from "../service/api";
import useAuthStore from "../store/auth";
import { showToast } from "../utils/toast";

type Panel =
  | "none"
  | "disable-user"
  | "delete-user"
  | "set-admin"
  | "disable-group"
  | "delete-group";

interface UserRow {
  uuid: string;
  nickname: string;
  telephone: string;
  is_admin: number;
  status: number;
  is_deleted?: boolean;
}

interface GroupRow {
  group_id: string;
  name: string;
  owner_id: string;
  member_cnt: number;
  avatar: string;
  status: number;
  is_deleted?: boolean;
}

const USER_PANELS: { panel: Panel; label: string }[] = [
  { panel: "disable-user", label: "Enable / Disable" },
  { panel: "delete-user", label: "Delete" },
  { panel: "set-admin", label: "Set as Admin" },
];

const GROUP_PANELS: { panel: Panel; label: string }[] = [
  { panel: "disable-group", label: "Enable / Disable" },
  { panel: "delete-group", label: "Delete / Disband" },
];

function MenuItem({
  active,
  label,
  onSelect,
}: {
  active: boolean;
  label: string;
  onSelect: () => void;
}) {
  return (
    <Button
      variant="ghost"
      aria-current={active ? "page" : undefined}
      className={`hover:bg-accent w-full justify-start rounded-none px-4 py-2 font-normal transition-colors ${
        active ? "bg-accent text-foreground" : "text-muted-foreground"
      }`}
      onClick={onSelect}
    >
      {label}
    </Button>
  );
}

function MenuSection({ title }: { title: string }) {
  return (
    <div className="text-primary px-4 pt-4 pb-1.5 text-xs font-semibold tracking-wider uppercase">
      {title}
    </div>
  );
}

export default function Manager() {
  const navigate = useNavigate();
  const [currentPanel, setCurrentPanel] = useState<Panel>("none");
  const [userList, setUserList] = useState<UserRow[]>([]);
  const [groupList, setGroupList] = useState<GroupRow[]>([]);
  const [selectedUserIds, setSelectedUserIds] = useState<string[]>([]);
  const [selectedGroupIds, setSelectedGroupIds] = useState<string[]>([]);

  const loadUserList = async () => {
    const uid = useAuthStore.getState().userInfo.uuid;
    const res = await api.getUserInfoList({ owner_id: uid });
    if (res.code !== 200) {
      showToast(res.message || "Failed to load users", "error");
      return;
    }
    setUserList((res.data as UserRow[]) || []);
    setSelectedUserIds([]);
  };

  const loadGroupList = async () => {
    const res = await api.getGroupInfoList({});
    if (res.code !== 200) {
      showToast(res.message || "Failed to load groups", "error");
      return;
    }
    setGroupList((res.data as GroupRow[]) || []);
    setSelectedGroupIds([]);
  };

  const showPanel = (panel: Panel) => {
    setCurrentPanel(panel);
    setSelectedUserIds([]);
    setSelectedGroupIds([]);
    if (
      panel === "disable-user" ||
      panel === "delete-user" ||
      panel === "set-admin"
    ) {
      loadUserList();
    } else if (panel === "disable-group" || panel === "delete-group") {
      loadGroupList();
    }
  };

  const toggleUser = (uuid: string, checked: boolean) => {
    setSelectedUserIds((prev) =>
      checked ? [...prev, uuid] : prev.filter((id) => id !== uuid),
    );
  };

  const toggleAllUsers = (checked: boolean) => {
    setSelectedUserIds(checked ? userList.map((u) => u.uuid) : []);
  };

  const toggleGroup = (uuid: string, checked: boolean) => {
    setSelectedGroupIds((prev) =>
      checked ? [...prev, uuid] : prev.filter((id) => id !== uuid),
    );
  };

  const toggleAllGroups = (checked: boolean) => {
    setSelectedGroupIds(checked ? groupList.map((g) => g.group_id) : []);
  };

  const requireSelection = (ids: string[], msg: string): boolean => {
    if (ids.length === 0) {
      showToast(msg, "warning");
      return false;
    }
    return true;
  };

  const runUserAction = async (
    action: () => Promise<{ code: number; message: string }>,
    successMsg: string,
  ) => {
    const res = await action();
    if (res.code === 200) {
      showToast(successMsg, "success");
      loadUserList();
    } else {
      showToast(res.message || "Operation failed", "error");
    }
  };

  const runGroupAction = async (
    action: () => Promise<{ code: number; message: string }>,
    successMsg: string,
  ) => {
    const res = await action();
    if (res.code === 200) {
      showToast(successMsg, "success");
      loadGroupList();
    } else {
      showToast(res.message || "Operation failed", "error");
    }
  };

  const enableSelectedUsers = async () => {
    if (!requireSelection(selectedUserIds, "No users selected")) return;
    await runUserAction(
      () => api.ableUsers({ uuid_list: selectedUserIds }),
      "Users enabled",
    );
  };

  const disableSelectedUsers = async () => {
    if (!requireSelection(selectedUserIds, "No users selected")) return;
    await runUserAction(
      () => api.disableUsers({ uuid_list: selectedUserIds }),
      "Users disabled",
    );
  };

  const deleteSelectedUsers = async () => {
    if (!requireSelection(selectedUserIds, "No users selected")) return;
    await runUserAction(
      () => api.deleteUsers({ uuid_list: selectedUserIds }),
      "Users deleted",
    );
  };

  const setAdminSelected = async (isAdmin: number) => {
    if (!requireSelection(selectedUserIds, "No users selected")) return;
    await runUserAction(
      () => api.setAdmin({ uuid_list: selectedUserIds, is_admin: isAdmin }),
      isAdmin ? "Admin granted" : "Admin revoked",
    );
  };

  const enableSelectedGroups = async () => {
    if (!requireSelection(selectedGroupIds, "No groups selected")) return;
    await runGroupAction(
      () => api.setGroupsStatus({ uuid_list: selectedGroupIds, status: 0 }),
      "Groups enabled",
    );
  };

  const disableSelectedGroups = async () => {
    if (!requireSelection(selectedGroupIds, "No groups selected")) return;
    await runGroupAction(
      () => api.setGroupsStatus({ uuid_list: selectedGroupIds, status: 1 }),
      "Groups disabled",
    );
  };

  const deleteSelectedGroups = async () => {
    if (!requireSelection(selectedGroupIds, "No groups selected")) return;
    await runGroupAction(
      () => api.deleteGroups({ uuid_list: selectedGroupIds }),
      "Groups deleted",
    );
  };

  const backToChat = () => navigate("/chat/sessions");

  const isUserPanel =
    currentPanel === "disable-user" ||
    currentPanel === "delete-user" ||
    currentPanel === "set-admin";
  const isGroupPanel =
    currentPanel === "disable-group" || currentPanel === "delete-group";
  const allUsersChecked =
    userList.length > 0 && selectedUserIds.length === userList.length;
  const allGroupsChecked =
    groupList.length > 0 && selectedGroupIds.length === groupList.length;

  return (
    <div className="bg-background flex min-h-screen items-center justify-center p-4">
      <Card className="shadow-primary/5 h-[600px] w-[1000px] gap-0 py-0 shadow-xl">
        <div className="border-border bg-muted/30 flex h-14 shrink-0 items-center justify-between border-b px-6">
          <div className="flex items-center gap-3">
            <Shield size={24} className="text-primary" />
            <span className="text-foreground text-lg font-semibold">
              Admin Panel
            </span>
          </div>
          <Button variant="ghost" size="sm" onClick={backToChat}>
            Back
          </Button>
        </div>

        <div className="flex flex-1 overflow-hidden">
          <div className="border-border bg-muted/30 w-48 shrink-0 overflow-y-auto border-r">
            <MenuSection title="Users" />
            <div className="flex flex-col gap-0.5 px-2">
              {USER_PANELS.map(({ panel, label }) => (
                <MenuItem
                  key={panel}
                  active={currentPanel === panel}
                  label={label}
                  onSelect={() => showPanel(panel)}
                />
              ))}
            </div>
            <MenuSection title="Groups" />
            <div className="flex flex-col gap-0.5 px-2">
              {GROUP_PANELS.map(({ panel, label }) => (
                <MenuItem
                  key={panel}
                  active={currentPanel === panel}
                  label={label}
                  onSelect={() => showPanel(panel)}
                />
              ))}
            </div>
          </div>

          <div className="flex-1 overflow-y-auto">
            {currentPanel === "none" && (
              <div className="flex h-full items-center justify-center">
                <p className="text-muted-foreground text-sm">
                  Select an option from the left menu
                </p>
              </div>
            )}

            {isUserPanel && (
              <div className="p-4">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-10">
                        <Checkbox
                          checked={allUsersChecked}
                          onCheckedChange={(checked) =>
                            toggleAllUsers(checked === true)
                          }
                          aria-label="Select all users"
                        />
                      </TableHead>
                      <TableHead>UUID</TableHead>
                      <TableHead>Nickname</TableHead>
                      <TableHead>Phone</TableHead>
                      <TableHead>Admin</TableHead>
                      <TableHead>Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {userList.map((user) => (
                      <TableRow key={user.uuid} className="hover:bg-accent/50">
                        <TableCell>
                          <Checkbox
                            checked={selectedUserIds.includes(user.uuid)}
                            onCheckedChange={(checked) =>
                              toggleUser(user.uuid, checked === true)
                            }
                            aria-label={`Select user ${user.nickname}`}
                          />
                        </TableCell>
                        <TableCell className="text-muted-foreground font-mono text-xs">
                          {user.uuid}
                        </TableCell>
                        <TableCell>{user.nickname}</TableCell>
                        <TableCell>{user.telephone}</TableCell>
                        <TableCell>
                          {user.is_admin === 1 ? (
                            <Badge
                              variant="secondary"
                              className="bg-success/15 text-success"
                            >
                              Yes
                            </Badge>
                          ) : (
                            <Badge variant="outline">No</Badge>
                          )}
                        </TableCell>
                        <TableCell>
                          {user.status === 1 ? (
                            <Badge
                              variant="destructive"
                              className="bg-destructive/15 text-destructive border-0"
                            >
                              Disabled
                            </Badge>
                          ) : (
                            <Badge className="bg-success/15 text-success border-0">
                              Active
                            </Badge>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <div className="mt-4 flex justify-end gap-2">
                  {currentPanel === "disable-user" && (
                    <>
                      <Button onClick={enableSelectedUsers}>Enable</Button>
                      <Button
                        variant="destructive"
                        onClick={disableSelectedUsers}
                      >
                        Disable
                      </Button>
                    </>
                  )}
                  {currentPanel === "delete-user" && (
                    <Button variant="destructive" onClick={deleteSelectedUsers}>
                      Delete
                    </Button>
                  )}
                  {currentPanel === "set-admin" && (
                    <>
                      <Button onClick={() => setAdminSelected(1)}>
                        Grant Admin
                      </Button>
                      <Button
                        variant="ghost"
                        onClick={() => setAdminSelected(0)}
                      >
                        Revoke Admin
                      </Button>
                    </>
                  )}
                </div>
              </div>
            )}

            {isGroupPanel && (
              <div className="p-4">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-10">
                        <Checkbox
                          checked={allGroupsChecked}
                          onCheckedChange={(checked) =>
                            toggleAllGroups(checked === true)
                          }
                          aria-label="Select all groups"
                        />
                      </TableHead>
                      <TableHead>Group ID</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Owner</TableHead>
                      <TableHead>Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {groupList.map((group) => (
                      <TableRow
                        key={group.group_id}
                        className="hover:bg-accent/50"
                      >
                        <TableCell>
                          <Checkbox
                            checked={selectedGroupIds.includes(group.group_id)}
                            onCheckedChange={(checked) =>
                              toggleGroup(group.group_id, checked === true)
                            }
                            aria-label={`Select group ${group.name}`}
                          />
                        </TableCell>
                        <TableCell className="text-muted-foreground font-mono text-xs">
                          {group.group_id}
                        </TableCell>
                        <TableCell>{group.name}</TableCell>
                        <TableCell className="text-muted-foreground font-mono text-xs">
                          {group.owner_id}
                        </TableCell>
                        <TableCell>
                          {group.is_deleted ? (
                            <Badge variant="outline">Deleted</Badge>
                          ) : group.status === 1 ? (
                            <Badge
                              variant="destructive"
                              className="bg-destructive/15 text-destructive border-0"
                            >
                              Disabled
                            </Badge>
                          ) : (
                            <Badge className="bg-success/15 text-success border-0">
                              Active
                            </Badge>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <div className="mt-4 flex justify-end gap-2">
                  {currentPanel === "disable-group" && (
                    <>
                      <Button onClick={enableSelectedGroups}>Enable</Button>
                      <Button
                        variant="destructive"
                        onClick={disableSelectedGroups}
                      >
                        Disable
                      </Button>
                    </>
                  )}
                  {currentPanel === "delete-group" && (
                    <Button
                      variant="destructive"
                      onClick={deleteSelectedGroups}
                    >
                      Delete
                    </Button>
                  )}
                </div>
              </div>
            )}
          </div>
        </div>
      </Card>
    </div>
  );
}
