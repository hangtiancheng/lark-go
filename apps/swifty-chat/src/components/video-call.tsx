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

import { Phone, PhoneOff, Video } from "lucide-react";
import { useEffect, useImperativeHandle, useRef, useState } from "react";
import type { Ref } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import useWsStore from "@/store/ws";
import { RtcManager } from "@/utils/rtc";

const NO_ANSWER_TIMEOUT_MS = 30_000;

export interface VideoCallHandle {
  show: () => void;
}

interface VideoCallProps {
  contactId: string;
  sessionId: string;
  ref?: Ref<VideoCallHandle>;
}

export function VideoCall({ contactId, sessionId, ref }: VideoCallProps) {
  const [visible, setVisible] = useState(false);
  const [incoming, setIncoming] = useState(false);
  const [active, setActive] = useState(false);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [remoteStream, setRemoteStream] = useState<MediaStream | null>(null);

  const rtcRef = useRef<RtcManager | null>(null);
  const localVideoRef = useRef<HTMLVideoElement>(null);
  const remoteVideoRef = useRef<HTMLVideoElement>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useImperativeHandle(ref, () => ({ show: () => setVisible(true) }), []);

  const clearNoAnswerTimer = () => {
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
  };

  useEffect(() => {
    const rtc = new RtcManager();
    rtc.onLocalStream = setLocalStream;
    rtc.onRemoteStream = setRemoteStream;
    rtc.onCallEnded = () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
        timeoutRef.current = null;
      }
      setVisible(false);
      setIncoming(false);
      setActive(false);
      setLocalStream(null);
      setRemoteStream(null);
    };
    rtcRef.current = rtc;

    // The socket store fans every `type: 3` frame out to its subscribers.
    const unsubscribe = useWsStore.getState().subscribeToSignals((signal) => {
      if (rtc.handleSignal(signal) === "incoming_call") {
        setVisible(true);
        setIncoming(true);
      }
    });

    return () => {
      unsubscribe();
      // Release the camera without calling back into an unmounted component.
      rtc.onCallEnded = null;
      rtc.endCall();
      rtcRef.current = null;
    };
  }, []);

  useEffect(() => {
    rtcRef.current?.setTarget({ sessionId, receiveId: contactId });
  }, [sessionId, contactId]);

  useEffect(() => {
    if (localVideoRef.current && localStream) {
      localVideoRef.current.srcObject = localStream;
    }
  }, [localStream]);

  useEffect(() => {
    if (remoteVideoRef.current && remoteStream) {
      remoteVideoRef.current.srcObject = remoteStream;
    }
  }, [remoteStream]);

  const startCall = async () => {
    const rtc = rtcRef.current;
    if (!rtc) return;
    setActive(true);
    await rtc.startCall();
    clearNoAnswerTimer();
    timeoutRef.current = setTimeout(() => {
      rtc.sendEndCall();
    }, NO_ANSWER_TIMEOUT_MS);
  };

  const acceptCall = async () => {
    clearNoAnswerTimer();
    setIncoming(false);
    setActive(true);
    await rtcRef.current?.acceptCall();
  };

  const rejectCall = () => {
    const rtc = rtcRef.current;
    if (!rtc) return;
    rtc.rejectCall();
    rtc.endCall();
  };

  const hangUp = () => rtcRef.current?.sendEndCall();

  return (
    <Dialog
      open={visible}
      onOpenChange={(next) => {
        if (next) return;
        if (active || incoming) hangUp();
        else setVisible(false);
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {incoming ? "Incoming call" : active ? "In call" : "Video call"}
          </DialogTitle>
        </DialogHeader>

        <div className="grid grid-cols-2 gap-2">
          <div className="bg-muted relative aspect-video overflow-hidden rounded-lg">
            <video
              ref={remoteVideoRef}
              autoPlay
              playsInline
              className={cn(
                "size-full object-cover",
                !remoteStream && "hidden",
              )}
            />
            {!remoteStream && (
              <span className="text-muted-foreground absolute inset-0 flex items-center justify-center text-xs">
                Waiting for the other side…
              </span>
            )}
          </div>
          <div className="bg-muted relative aspect-video overflow-hidden rounded-lg">
            <video
              ref={localVideoRef}
              autoPlay
              playsInline
              muted
              className={cn("size-full object-cover", !localStream && "hidden")}
            />
            {!localStream && (
              <span className="text-muted-foreground absolute inset-0 flex items-center justify-center text-xs">
                Your camera is off
              </span>
            )}
          </div>
        </div>

        <div className="flex justify-center gap-2">
          {incoming ? (
            <>
              <Button size="sm" onClick={() => void acceptCall()}>
                <Phone className="size-3.5" />
                Accept
              </Button>
              <Button size="sm" variant="destructive" onClick={rejectCall}>
                <PhoneOff className="size-3.5" />
                Reject
              </Button>
            </>
          ) : active ? (
            <Button size="sm" variant="destructive" onClick={hangUp}>
              <PhoneOff className="size-3.5" />
              Hang Up
            </Button>
          ) : (
            <>
              <Button size="sm" onClick={() => void startCall()}>
                <Video className="size-3.5" />
                Start Call
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setVisible(false)}
              >
                Close
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
