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
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/service/api";
import useAuthStore from "@/store/auth";
import useWsStore from "@/store/ws";
import { isValidPhone } from "@/utils/validate";
import { showToast } from "@/utils/toast";
import type { UserInfo } from "@/types";

export default function Login() {
  const [telephone, setTelephone] = useState("");
  const [password, setPassword] = useState("");
  const navigate = useNavigate();

  const handleLogin = async () => {
    if (!telephone || !password) {
      showToast("Please fill in all fields", "error");
      return;
    }
    if (!isValidPhone(telephone)) {
      showToast("Invalid phone number", "error");
      return;
    }

    const res = (await api.login({ telephone, password })) as {
      code: number;
      message: string;
      data: UserInfo;
    };
    if (res.code === 200) {
      if (res.data && res.data.status === 1) {
        showToast("This account has been banned", "error");
        return;
      }
      showToast(res.message, "success");
      useAuthStore.getState().setUserInfo(res.data);
      useWsStore.getState().connect(res.data.uuid);
      navigate("/chat/sessions");
    } else {
      showToast(res.message || "Login failed", "error");
    }
  };

  return (
    <div className="bg-background relative flex min-h-screen items-center justify-center overflow-hidden p-4">
      {/* Ambient soft-pink layers */}
      <div
        aria-hidden
        className="bg-primary/10 pointer-events-none absolute -top-32 -left-32 h-96 w-96 animate-pulse rounded-full blur-3xl [animation-duration:7s] motion-reduce:animate-none"
      />
      <div
        aria-hidden
        className="bg-primary/5 pointer-events-none absolute -right-24 -bottom-40 h-[28rem] w-[28rem] animate-pulse rounded-full blur-3xl [animation-delay:1.5s] [animation-duration:9s] motion-reduce:animate-none"
      />
      <div
        aria-hidden
        className="bg-primary/[0.07] pointer-events-none absolute top-1/4 right-1/3 h-64 w-64 rounded-full blur-3xl"
      />

      <Card className="animate-in fade-in zoom-in-95 shadow-primary/5 w-full max-w-md shadow-xl duration-300">
        <CardHeader>
          <CardTitle className="text-2xl font-semibold tracking-tight">
            Sign In
          </CardTitle>
          <CardDescription>Welcome back to Swifty Chat</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Label htmlFor="login-phone">Phone</Label>
            <Input
              id="login-phone"
              type="text"
              placeholder="Enter your phone number"
              value={telephone}
              onChange={(e) => setTelephone(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="login-password">Password</Label>
            <Input
              id="login-password"
              type="password"
              placeholder="Enter your password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
        </CardContent>
        <CardFooter className="flex-col gap-3">
          <Button className="w-full" onClick={handleLogin}>
            Sign In
          </Button>
          <div className="flex w-full justify-end">
            <a
              className="text-primary cursor-pointer text-sm hover:underline"
              onClick={() => navigate("/register")}
            >
              Register
            </a>
          </div>
        </CardFooter>
      </Card>
    </div>
  );
}
