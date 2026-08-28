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

import { MessageType } from "@/service/schemas";
import useAuthStore from "@/store/auth";
import useWsStore from "@/store/ws";

/** Who the signalling frames are addressed to. */
export interface CallTarget {
  sessionId: string;
  receiveId: string;
}

export class RtcManager {
  pc: RTCPeerConnection | null = null;
  localStream: MediaStream | null = null;
  remoteStream: MediaStream | null = null;
  onLocalStream: ((stream: MediaStream) => void) | null = null;
  onRemoteStream: ((stream: MediaStream) => void) | null = null;
  onCallEnded: (() => void) | null = null;

  private target: CallTarget = { sessionId: "", receiveId: "" };

  setTarget(target: CallTarget) {
    this.target = target;
  }

  /** Signalling rides on a normal chat frame with `type: 3`. */
  private sendFrame(payload: Record<string, unknown>) {
    const { userInfo } = useAuthStore.getState();
    if (!this.target.receiveId) return;
    useWsStore.getState().send({
      session_id: this.target.sessionId,
      type: MessageType.AvSignal,
      content: "",
      url: "",
      send_id: userInfo.uuid,
      send_name: userInfo.nickname,
      send_avatar: userInfo.avatar,
      receive_id: this.target.receiveId,
      file_size: "",
      file_name: "",
      file_type: "",
      av_data: JSON.stringify(payload),
    });
  }

  private sendSignal(type: string, data?: Record<string, unknown>) {
    this.sendFrame({
      messageId: "PROXY",
      type,
      ...(data ? { messageData: data } : {}),
    });
  }

  createPeerConnection() {
    if (this.pc) return;
    this.pc = new RTCPeerConnection({});
    this.pc.onicecandidate = (event) => {
      if (event.candidate) {
        this.sendSignal("candidate", { candidate: event.candidate });
      }
    };
    this.pc.ontrack = (event) => {
      if (!this.remoteStream) {
        this.remoteStream = new MediaStream();
        this.onRemoteStream?.(this.remoteStream);
      }
      this.remoteStream.addTrack(event.track);
    };
  }

  async getLocalMedia() {
    if (this.localStream) return this.localStream;
    this.localStream = await navigator.mediaDevices.getUserMedia({
      video: true,
      audio: true,
    });
    this.onLocalStream?.(this.localStream);
    return this.localStream;
  }

  attachLocalToPeer() {
    if (!this.localStream || !this.pc) return;
    for (const track of this.localStream.getTracks()) {
      this.pc.addTrack(track);
    }
  }

  async startCall() {
    this.createPeerConnection();
    await this.getLocalMedia();
    this.attachLocalToPeer();
    this.sendSignal("start_call");
  }

  async acceptCall() {
    this.createPeerConnection();
    await this.getLocalMedia();
    this.attachLocalToPeer();
    this.sendSignal("receive_call");
  }

  rejectCall() {
    this.sendSignal("reject_call");
  }

  async createOffer() {
    if (!this.pc) return;
    const description = await this.pc.createOffer({
      offerToReceiveAudio: true,
      offerToReceiveVideo: true,
    });
    await this.pc.setLocalDescription(description);
    this.sendSignal("sdp", { sdp: description });
  }

  async handleOfferSdp(sdp: RTCSessionDescriptionInit) {
    if (!this.pc) return;
    await this.pc.setRemoteDescription(new RTCSessionDescription(sdp));
    const answer = await this.pc.createAnswer();
    await this.pc.setLocalDescription(answer);
    this.sendSignal("sdp", { sdp: answer });
  }

  async handleAnswerSdp(sdp: RTCSessionDescriptionInit) {
    if (!this.pc) return;
    await this.pc.setRemoteDescription(new RTCSessionDescription(sdp));
  }

  handleCandidate(candidate: RTCIceCandidateInit) {
    void this.pc?.addIceCandidate(new RTCIceCandidate(candidate));
  }

  endCall() {
    this.localStream?.getTracks().forEach((track) => track.stop());
    this.pc?.close();
    this.localStream = null;
    this.remoteStream = null;
    this.pc = null;
    this.onCallEnded?.();
  }

  sendEndCall() {
    this.sendFrame({ messageId: "PEER_LEAVE" });
    this.endCall();
  }

  /** Returns "incoming_call" so the caller can raise its ringing UI. */
  handleSignal(avData: Record<string, unknown>) {
    const messageId = avData.messageId as string;
    const type = avData.type as string | undefined;
    const messageData = avData.messageData as
      Record<string, unknown> | undefined;

    if (messageId === "PEER_LEAVE") {
      this.endCall();
      return undefined;
    }
    if (messageId !== "PROXY") return undefined;

    if (type === "start_call") {
      return "incoming_call" as const;
    }
    if (type === "receive_call") {
      void this.createOffer();
    } else if (type === "reject_call" || type === "call_failed") {
      this.endCall();
    } else if (type === "sdp" && messageData) {
      const sdp = messageData.sdp as RTCSessionDescriptionInit;
      if (sdp.type === "offer") void this.handleOfferSdp(sdp);
      else if (sdp.type === "answer") void this.handleAnswerSdp(sdp);
    } else if (type === "candidate" && messageData) {
      this.handleCandidate(messageData.candidate as RTCIceCandidateInit);
    }
    return undefined;
  }
}
