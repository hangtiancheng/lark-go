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

import useWsStore from "../store/ws";
import useChatStore from "../store/chat";
import useAuthStore from "../store/auth";

export class RtcManager {
  pc: RTCPeerConnection | null = null;
  localStream: MediaStream | null = null;
  remoteStream: MediaStream | null = null;
  onLocalStream: ((s: MediaStream) => void) | null = null;
  onRemoteStream: ((s: MediaStream) => void) | null = null;
  onCallEnded: (() => void) | null = null;

  private sendSignal(type: string, data?: Record<string, unknown>) {
    const auth = useAuthStore.getState();
    const chat = useChatStore.getState();
    const payload: Record<string, unknown> = {
      messageId: "PROXY",
      type,
      ...(data ? { messageData: data } : {}),
    };
    useWsStore.getState().send({
      session_id: chat.sessionId,
      type: 3,
      content: "",
      url: "",
      send_id: auth.userInfo.uuid,
      send_name: auth.userInfo.nickname,
      send_avatar: auth.userInfo.avatar,
      receive_id: chat.contactInfo!.contact_id,
      file_size: "",
      file_name: "",
      file_type: "",
      av_data: JSON.stringify(payload),
    });
  }

  createPeerConnection() {
    if (this.pc) return;
    this.pc = new RTCPeerConnection({});
    this.pc.onicecandidate = (e) => {
      if (e.candidate) this.sendSignal("candidate", { candidate: e.candidate });
    };
    this.pc.ontrack = (e) => {
      if (!this.remoteStream) {
        this.remoteStream = new MediaStream();
        this.onRemoteStream?.(this.remoteStream);
      }
      this.remoteStream.addTrack(e.track);
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
    this.localStream.getTracks().forEach((t) => this.pc!.addTrack(t));
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
    const desc = await this.pc.createOffer({
      offerToReceiveAudio: true,
      offerToReceiveVideo: true,
    });
    await this.pc.setLocalDescription(desc);
    this.sendSignal("sdp", { sdp: desc });
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
    this.pc?.addIceCandidate(new RTCIceCandidate(candidate));
  }

  endCall() {
    this.localStream?.getTracks().forEach((t) => t.stop());
    this.pc?.close();
    this.localStream = null;
    this.remoteStream = null;
    this.pc = null;
    this.onCallEnded?.();
  }

  sendEndCall() {
    const payload = { messageId: "PEER_LEAVE" };
    const auth = useAuthStore.getState();
    const chat = useChatStore.getState();
    useWsStore.getState().send({
      session_id: chat.sessionId,
      type: 3,
      content: "",
      url: "",
      send_id: auth.userInfo.uuid,
      send_name: auth.userInfo.nickname,
      send_avatar: auth.userInfo.avatar,
      receive_id: chat.contactInfo!.contact_id,
      file_size: "",
      file_name: "",
      file_type: "",
      av_data: JSON.stringify(payload),
    });
    this.endCall();
  }

  handleSignal(avData: Record<string, unknown>) {
    const msgId = avData.messageId as string;
    const type = avData.type as string | undefined;
    const msgData = avData.messageData as Record<string, unknown> | undefined;

    if (msgId === "PEER_LEAVE") {
      this.endCall();
      return;
    }
    if (msgId !== "PROXY") return;

    if (type === "start_call") {
      return "incoming_call" as const;
    } else if (type === "receive_call") {
      this.createOffer();
    } else if (type === "reject_call") {
      this.endCall();
    } else if (type === "sdp" && msgData) {
      const sdp = msgData.sdp as RTCSessionDescriptionInit;
      if (sdp.type === "offer") this.handleOfferSdp(sdp);
      else if (sdp.type === "answer") this.handleAnswerSdp(sdp);
    } else if (type === "candidate" && msgData) {
      this.handleCandidate(msgData.candidate as RTCIceCandidateInit);
    }
    return undefined;
  }
}
