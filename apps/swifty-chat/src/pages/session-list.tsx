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

import { useNavigate } from "react-router-dom";
import { MessageSquare } from "lucide-react";
import { Card } from "@/components/ui/card";
import { NavBar } from "@/components/nav-bar";
import { SessionSidebar } from "@/components/session-sidebar";
import useAuthStore from "@/store/auth";
import { performLogout } from "@/utils/logout";

export default function SessionList() {
  const navigate = useNavigate();
  const userInfo = useAuthStore((s) => s.userInfo);

  const handleLogout = async () => {
    await performLogout();
    navigate("/login");
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
          <SessionSidebar onChat={(id) => navigate(`/chat/${id}`)} />
        </div>
        <div className="text-muted-foreground/50 flex flex-1 flex-col items-center justify-center">
          <MessageSquare size={64} strokeWidth={1.5} className="mb-4" />
          <p className="text-muted-foreground/70">
            Select a conversation to start chatting
          </p>
        </div>
      </Card>
    </div>
  );
}
