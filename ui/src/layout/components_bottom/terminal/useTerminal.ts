import { useEffect } from "react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useRTCStore, useSerialStore, useSettingsStore } from "@/hooks/stores";

class WebSocketDataChannel extends EventTarget {
  binaryType: BinaryType = "arraybuffer";
  bufferedAmount = 0;
  id: number | null = null;
  label: string;
  maxPacketLifeTime: number | null = null;
  maxRetransmits: number | null = null;
  negotiated = false;
  ordered = true;
  protocol = "";

  private ws: WebSocket;
  private state: RTCDataChannelState = "connecting";

  constructor(label: string, url: string) {
    super();
    this.label = label;

    this.ws = new WebSocket(url);
    this.ws.binaryType = this.binaryType;

    this.ws.addEventListener("open", () => {
      this.state = "open";
      this.dispatchEvent(new Event("open"));
    });

    this.ws.addEventListener("close", () => {
      this.state = "closed";
      this.dispatchEvent(new Event("close"));
    });

    this.ws.addEventListener("message", e => {
      this.dispatchEvent(new MessageEvent("message", { data: e.data }));
    });

    this.ws.addEventListener("error", () => {
      this.state = "closing";
      this.dispatchEvent(new Event("error"));
    });
  }

  get readyState(): RTCDataChannelState {
    return this.state;
  }

  close(): void {
    if (this.state === "closed") return;
    this.state = "closing";
    this.ws.close();
  }

  send(data: string | ArrayBuffer | Blob | ArrayBufferView): void {
    if (this.ws.readyState !== WebSocket.OPEN) return;
    if (typeof data === "string") {
      this.ws.send(data);
      return;
    }
    if (data instanceof Blob) {
      this.ws.send(data);
      return;
    }
    if (data instanceof ArrayBuffer) {
      this.ws.send(data);
      return;
    }
    this.ws.send(data);
  }
}

const buildWsUrl = (path: string) => {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}${path}`;
};

export const useTerminal = () => {
  const peerConnection = useRTCStore(state => state.peerConnection);
  const serialConsole = useRTCStore(state => state.serialConsole);
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const setSerialConsole = useRTCStore(state => state.setSerialConsole);
  const setKvmTerminal = useRTCStore(state => state.setKvmTerminal);
  const setIsConnected = useSerialStore(state => state.setIsConnected);
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const [send] = useJsonRpc();

  // The backend owns the serial port state, so ask it whenever the serial
  // channel comes up instead of guessing from the local UI state.
  useEffect(() => {
    if (!serialConsole) return;

    const rpcReady = () => forceHttp || rpcDataChannel?.readyState === "open";

    const syncSerialStatus = () => {
      // The status query would be dropped silently while the RPC channel is
      // still connecting, so wait for it.
      if (!rpcReady()) return;

      send("getSerialSettings", {}, resp => {
        if ("error" in resp) return;
        const settings = resp.result as { connected?: boolean };
        setIsConnected(settings?.connected === true);
      });
    };

    syncSerialStatus();
    serialConsole.addEventListener("open", syncSerialStatus);
    rpcDataChannel?.addEventListener("open", syncSerialStatus);

    return () => {
      serialConsole.removeEventListener("open", syncSerialStatus);
      rpcDataChannel?.removeEventListener("open", syncSerialStatus);
    };
  }, [serialConsole, rpcDataChannel, forceHttp, send, setIsConnected]);

  useEffect(() => {
    let cleanup: (() => void) | null = null;

    if (forceHttp) {
      const kvmTerminal = new WebSocketDataChannel("terminal", buildWsUrl("/terminal/ws"));
      const serialConsole = new WebSocketDataChannel("serial", buildWsUrl("/serial/ws"));

      setKvmTerminal(kvmTerminal as unknown as RTCDataChannel);
      setSerialConsole(serialConsole as unknown as RTCDataChannel);

      cleanup = () => {
        kvmTerminal.close();
        serialConsole.close();
        setKvmTerminal(null);
        setSerialConsole(null);
      };
    } else if (peerConnection) {
      const kvmTerminal = peerConnection.createDataChannel("terminal");
      const serialConsole = peerConnection.createDataChannel("serial");

      setKvmTerminal(kvmTerminal);
      setSerialConsole(serialConsole);

      cleanup = () => {
        kvmTerminal.close();
        serialConsole.close();
        setKvmTerminal(null);
        setSerialConsole(null);
      };
    } else {
      setKvmTerminal(null);
      setSerialConsole(null);
    }

    return () => {
      cleanup?.();
    };
  }, [forceHttp, peerConnection, setKvmTerminal, setSerialConsole]);
};
