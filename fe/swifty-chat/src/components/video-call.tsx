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

import {
  forwardRef,
  useImperativeHandle,
  useRef,
  useState,
  useEffect,
} from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { RtcManager } from "@/utils/rtc";
import { showToast } from "@/utils/toast";

export interface VideoCallHandle {
  show: () => void;
  handleSignal: (avData: Record<string, unknown>) => void;
}

export const VideoCall = forwardRef<VideoCallHandle>(
  function VideoCall(_, ref) {
    const [visible, setVisible] = useState(false);
    const [inCall, setInCall] = useState(false);
    const [incomingCall, setIncomingCall] = useState(false);

    const localVideoRef = useRef<HTMLVideoElement>(null);
    const remoteVideoRef = useRef<HTMLVideoElement>(null);
    const [rtc] = useState(() => new RtcManager());

    const OUTGOING_CALL_TIMEOUT_MS = 30_000;
    const callTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const clearCallTimeout = () => {
      if (callTimeoutRef.current) {
        clearTimeout(callTimeoutRef.current);
        callTimeoutRef.current = null;
      }
    };

    useEffect(() => {
      rtc.onLocalStream = (stream) => {
        if (localVideoRef.current) localVideoRef.current.srcObject = stream;
      };
      rtc.onRemoteStream = (stream) => {
        if (remoteVideoRef.current) remoteVideoRef.current.srcObject = stream;
      };
      rtc.onCallEnded = () => {
        clearCallTimeout();
        setInCall(false);
        setIncomingCall(false);
        showToast("Call ended", "info");
      };
      return () => {
        // Detach callbacks first so unmount cleanup does not toast or update state.
        rtc.onLocalStream = null;
        rtc.onRemoteStream = null;
        rtc.onCallEnded = null;
        clearCallTimeout();
        rtc.endCall();
      };
    }, [rtc]);

    // Re-attach streams if the overlay opens while tracks already exist.
    useEffect(() => {
      if (!visible) return;
      if (rtc.localStream && localVideoRef.current) {
        localVideoRef.current.srcObject = rtc.localStream;
      }
      if (rtc.remoteStream && remoteVideoRef.current) {
        remoteVideoRef.current.srcObject = rtc.remoteStream;
      }
    }, [visible, inCall, rtc]);

    useImperativeHandle(
      ref,
      () => ({
        show: () => setVisible(true),
        handleSignal: (avData: Record<string, unknown>) => {
          const signalType = avData.type as string | undefined;
          if (signalType === "receive_call" || signalType === "reject_call") {
            clearCallTimeout();
          }
          const result = rtc.handleSignal(avData);
          if (result === "incoming_call") {
            setVisible(true);
            setIncomingCall(true);
            showToast("Incoming call", "info");
          }
        },
      }),
      [rtc],
    );

    const startCall = async () => {
      await rtc.startCall();
      setInCall(true);
      clearCallTimeout();
      callTimeoutRef.current = setTimeout(() => {
        callTimeoutRef.current = null;
        rtc.sendEndCall();
        setInCall(false);
        showToast("No answer", "warning");
      }, OUTGOING_CALL_TIMEOUT_MS);
    };

    const acceptCall = async () => {
      await rtc.acceptCall();
      setInCall(true);
      setIncomingCall(false);
    };

    const rejectCall = () => {
      rtc.rejectCall();
      setIncomingCall(false);
    };

    const hangUp = () => {
      clearCallTimeout();
      rtc.sendEndCall();
      setInCall(false);
    };

    const closeModal = () => {
      if (inCall) {
        showToast("Please hang up first", "warning");
        return;
      }
      setVisible(false);
      setIncomingCall(false);
    };

    if (!visible) return null;

    return (
      <div className="bg-foreground/40 fixed inset-0 z-50 flex items-center justify-center backdrop-blur-sm">
        <Card className="animate-in fade-in zoom-in-95 w-[700px] max-w-[90vw] p-6 shadow-2xl duration-200">
          <CardHeader>
            <CardTitle className="text-center text-lg font-semibold">
              Video Call
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex justify-center gap-4">
              <div className="bg-muted relative h-56 w-72 overflow-hidden rounded-lg">
                <video
                  ref={localVideoRef}
                  autoPlay
                  playsInline
                  muted
                  className="h-full w-full object-cover"
                />
                <Badge variant="secondary" className="absolute bottom-2 left-2">
                  You
                </Badge>
              </div>
              <div className="bg-muted relative h-56 w-72 overflow-hidden rounded-lg">
                <video
                  ref={remoteVideoRef}
                  autoPlay
                  playsInline
                  className="h-full w-full object-cover"
                />
                <Badge variant="secondary" className="absolute bottom-2 left-2">
                  Remote
                </Badge>
              </div>
            </div>

            {incomingCall && (
              <p className="text-primary text-center text-sm">
                Incoming call...
              </p>
            )}

            <div className="flex justify-center gap-3">
              {!inCall && !incomingCall && (
                <Button size="sm" onClick={startCall}>
                  Start Call
                </Button>
              )}
              {incomingCall && (
                <>
                  <Button size="sm" onClick={acceptCall}>
                    Accept
                  </Button>
                  <Button size="sm" variant="destructive" onClick={rejectCall}>
                    Reject
                  </Button>
                </>
              )}
              {inCall && (
                <Button size="sm" variant="destructive" onClick={hangUp}>
                  Hang Up
                </Button>
              )}
              <Button
                size="sm"
                variant="ghost"
                className="text-muted-foreground"
                onClick={closeModal}
              >
                Close
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  },
);
