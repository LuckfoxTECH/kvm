import { useCallback, useEffect } from "react";

import { useRTCStore, useSettingsStore } from "@/hooks/stores";

export interface JsonRpcRequest {
  jsonrpc: string;
  method: string;
  params: object;
  id?: number | string;
}

export interface JsonRpcError {
  code: number;
  data?: string;
  message: string;
}

export interface JsonRpcSuccessResponse {
  jsonrpc: string;
  result: boolean | number | object | string | [];
  id: string | number;
}

export interface JsonRpcErrorResponse {
  jsonrpc: string;
  error: JsonRpcError;
  id: string | number;
}

export type JsonRpcResponse = JsonRpcSuccessResponse | JsonRpcErrorResponse;

const callbackStore = new Map<number | string, (resp: JsonRpcResponse) => void>();
let requestCounter = 0;
let httpSessionId: string | null = null;
let httpSessionInvalidated = false;

function getHttpSessionId() {
  if (httpSessionId) return httpSessionId;
  try {
    const existing = window.sessionStorage.getItem("httpSessionId");
    if (existing) {
      httpSessionId = existing;
      return httpSessionId;
    }
    const generated = typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    window.sessionStorage.setItem("httpSessionId", generated);
    httpSessionId = generated;
    return httpSessionId;
  } catch {
    const fallback = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    httpSessionId = fallback;
    return httpSessionId;
  }
}

export function resetHttpSessionId() {
  try {
    window.sessionStorage.removeItem("httpSessionId");
  } catch {
    void 0;
  }
  httpSessionId = null;
  httpSessionInvalidated = true;
}

export function useJsonRpc(onRequest?: (payload: JsonRpcRequest) => void) {
  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const forceHttp = useSettingsStore(state => state.forceHttp);

  const send = useCallback(
    (method: string, params: unknown, callback?: (resp: JsonRpcResponse) => void) => {
      // The RPC data channel exists only while the WebRTC video session is up.
      // Power and the other device calls still have to reach the KVM when the
      // target PC is off, so use the HTTP API instead of dropping them.
      if (forceHttp || !rpcDataChannel || rpcDataChannel.readyState !== "open") {
        if (httpSessionInvalidated) {
          requestCounter++;
          const payloadId = requestCounter;
          if (callback) {
            callback({
              jsonrpc: "2.0",
              error: { code: -32002, message: "HTTP session invalidated on client" },
              id: payloadId,
            } as JsonRpcErrorResponse);
          }
          return;
        }
        requestCounter++;
        const payload = { jsonrpc: "2.0", method, params, id: requestCounter };

        fetch("/api/rpc", {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Session-ID': getHttpSessionId(),
          },
          body: JSON.stringify(payload)
        })
        .then(res => res.json())
        .then((data: unknown) => {
          const handleEvent = (event: JsonRpcRequest) => {
            if (event.method === "refreshPage") {
              const currentUrl = new URL(window.location.href);
              currentUrl.searchParams.set("networkChanged", "true");
              window.location.href = currentUrl.toString();
              return;
            }
            if (onRequest) onRequest(event);
          };

          if (data && typeof data === "object" && ("response" in data || "event" in data)) {
            const wrapper = data as { response: JsonRpcResponse; event?: JsonRpcRequest };
            if (wrapper.event) {
              handleEvent(wrapper.event);
            }
            if (callback) callback(wrapper.response);
            return;
          }

          if (data && typeof data === "object" && "method" in data) {
            handleEvent(data as JsonRpcRequest);
            return;
          }

          if (callback) callback(data as JsonRpcResponse);
        })
        .catch(err => {
          console.error("RPC over HTTP failed", err);
          if (callback) {
            callback({
              jsonrpc: "2.0",
              error: { code: -32000, message: "HTTP RPC failed", data: err.toString() },
              id: payload.id
            });
          }
        });
        return;
      }

      requestCounter++;
      const payload = { jsonrpc: "2.0", method, params, id: requestCounter };
      // Store the callback if it exists
      if (callback) callbackStore.set(payload.id, callback);

      rpcDataChannel.send(JSON.stringify(payload));
    },
    [rpcDataChannel, forceHttp, onRequest],
  );

  const sendNotification = useCallback(
    (method: string, params: unknown) => {
      const payload = { jsonrpc: "2.0", method, params };

      if (forceHttp || !rpcDataChannel || rpcDataChannel.readyState !== "open") {
        fetch("/api/rpc", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Session-ID": getHttpSessionId(),
          },
          body: JSON.stringify(payload),
        }).catch(err => {
          console.error("RPC notification over HTTP failed", err);
        });
        return;
      }

      rpcDataChannel.send(JSON.stringify(payload));
    },
    [rpcDataChannel, forceHttp],
  );

  useEffect(() => {
    if (!rpcDataChannel) return;

    const messageHandler = (e: MessageEvent) => {
      const payload = JSON.parse(e.data) as JsonRpcResponse | JsonRpcRequest;

      // The "API" can also "request" data from the client
      // If the payload has a method, it's a request
      if ("method" in payload) {
        if ((payload as JsonRpcRequest).method === "refreshPage") {
          const currentUrl = new URL(window.location.href);
          currentUrl.searchParams.set("networkChanged", "true");
          window.location.href = currentUrl.toString();
          return;
        }
        if (onRequest) onRequest(payload as JsonRpcRequest);
        return;
      }

      if ("error" in payload) console.error(payload.error);
      if (!payload.id) return;

      const callback = callbackStore.get(payload.id);
      if (callback) {
        callback(payload);
        callbackStore.delete(payload.id);
      }
    };

    rpcDataChannel.addEventListener("message", messageHandler);

    return () => {
      rpcDataChannel.removeEventListener("message", messageHandler);
    };
  }, [rpcDataChannel, onRequest]);

  return [send, sendNotification] as const;
}
