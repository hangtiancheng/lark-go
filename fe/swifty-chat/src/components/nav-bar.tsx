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

import { LogOut, MessageSquare, Settings, User, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

interface NavBarProps {
  avatar: string;
  isAdmin: boolean;
  onNavigate: (path: string) => void;
  onLogout: () => void;
}

interface RailItem {
  label: string;
  path: string;
  icon: LucideIcon;
}

const NAV_ITEMS: RailItem[] = [
  { label: "Sessions", path: "/chat/sessions", icon: MessageSquare },
  { label: "Contacts", path: "/chat/contacts", icon: Users },
  { label: "Profile", path: "/chat/profile", icon: User },
];

interface RailButtonProps {
  label: string;
  icon: LucideIcon;
  onClick: () => void;
  destructive?: boolean;
}

function RailButton({
  label,
  icon: Icon,
  onClick,
  destructive,
}: RailButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            aria-label={label}
            onClick={onClick}
            className={cn(
              "transition-all duration-200 hover:scale-105 active:scale-95",
              destructive
                ? "text-destructive hover:bg-destructive/10 hover:text-destructive"
                : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
            )}
          />
        }
      >
        <Icon className="size-5" />
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
}

export function NavBar({ avatar, isAdmin, onNavigate, onLogout }: NavBarProps) {
  return (
    <nav className="border-border bg-muted/50 flex h-full w-16 flex-col items-center border-r py-4">
      <Avatar className="ring-primary/30 ring-offset-card size-10 ring-2 ring-offset-2 transition-transform duration-200 hover:scale-105">
        <AvatarImage src={avatar} alt="Your avatar" />
        <AvatarFallback>U</AvatarFallback>
      </Avatar>

      <div className="mt-6 flex flex-col items-center gap-1">
        {NAV_ITEMS.map((item) => (
          <RailButton
            key={item.path}
            label={item.label}
            icon={item.icon}
            onClick={() => onNavigate(item.path)}
          />
        ))}
      </div>

      <div className="flex-1" />

      <div className="flex flex-col items-center gap-1">
        <div className="bg-border mb-2 h-px w-8" aria-hidden="true" />
        {isAdmin && (
          <RailButton
            label="Admin"
            icon={Settings}
            onClick={() => onNavigate("/manager")}
          />
        )}
        <RailButton
          label="Sign Out"
          icon={LogOut}
          onClick={onLogout}
          destructive
        />
      </div>
    </nav>
  );
}
