import { useCallback, useEffect } from "react";

import { useVpnStore, type TunnelKey } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";

// Every one of these answers by running `pgrep` on the device, so they are polled
// only while the Remote drawer is mounted rather than for the whole session. The
// drawer is the only place that shows them, and the bar icon reuses the last
// values that were read, which keeps a closed drawer from costing anything.
const POLL_MS = 5000;

// Tunnels whose whole status is `{ running: bool }`.
const FLAG_TUNNELS: Array<{ key: TunnelKey; method: string }> = [
  { key: "wireguard", method: "getWireguardStatus" },
  { key: "easytier", method: "getEasyTierStatus" },
  { key: "vnt", method: "getVntStatus" },
  { key: "frpc", method: "getFrpcStatus" },
  { key: "cloudflared", method: "getCloudflaredStatus" },
];

// useRemoteTunnels fills the flag-only tunnel entries of the VPN store. Passing
// enabled=false leaves the last values in place and stops polling, which is what
// a drawer that stays mounted after closing needs.
export function useRemoteTunnels(enabled = true) {
  const [send] = useJsonRpc();
  const setTunnelEntry = useVpnStore(state => state.setTunnelEntry);

  const poll = useCallback(() => {
    FLAG_TUNNELS.forEach(({ key, method }) => {
      send(method, {}, resp => {
        if ("error" in resp) {
          // A firmware without this tunnel answers "method not found"; report it
          // as unknown instead of claiming the service is stopped.
          setTunnelEntry(key, { up: false, state: "unknown" });
          return;
        }
        const running = Boolean((resp.result as { running?: boolean } | null)?.running);
        setTunnelEntry(key, { up: running, state: running ? "connected" : "closed" });
      });
    });

    // NetBird returns more than a flag, so it maps field by field instead.
    send("getNetbirdStatus", {}, resp => {
      if ("error" in resp) {
        setTunnelEntry("netbird", { up: false, state: "unknown" });
        return;
      }
      const status = resp.result as
        | { running?: boolean; connected?: boolean; state?: string; ip?: string }
        | null;
      setTunnelEntry("netbird", {
        up: Boolean(status?.connected),
        state: status?.state || (status?.running ? "connecting" : "closed"),
        detail: status?.ip || undefined,
      });
    });
  }, [send, setTunnelEntry]);

  useEffect(() => {
    if (!enabled) return;

    poll();
    const timer = window.setInterval(poll, POLL_MS);
    return () => window.clearInterval(timer);
  }, [enabled, poll]);
}
