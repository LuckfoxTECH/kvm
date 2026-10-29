import React from "react";
import { useReactAt } from "i18n-auto-extractor/react";
import NetworkSVG from "@assets/second/network.svg?react";

import { useUiStore, useVpnStore, type TunnelEntry, type TunnelKey } from "@/hooks/stores";
import { useRemoteTunnels } from "@/hooks/useRemoteTunnels";
import { StatusPanel, StatusPanelRow } from "@/layout/core/bar_bottom/StatusPanel";

export interface RemoteStatusPanelProps {
  tailScaleState?: string;
  zeroTierState?: string;
  /** False when the drawer is mounted but not visible; polling pauses then. */
  active?: boolean;
}

const STATE_LABEL: Record<string, string> = {
  connected: "Connected",
  logined: "Logged in",
  connecting: "Connecting",
  starting: "Starting",
  needs_auth: "Needs login",
  connected_no_port: "Limited",
  disconnected: "Disconnected",
  closed: "Stopped",
  down: "Stopped",
  disabled: "Disabled",
};

const GREEN = "rgba(0, 205, 27, 1)";
const GREY = "rgba(205, 205, 205, 1)";

function tunnelUp(state?: string) {
  return state === "connected" || state === "logined";
}

// remoteIconColor colors the network icon in front of the Remote button. A
// single tunnel is enough to make remote access possible, so the icon turns
// green as soon as any overlay network is up, and stays grey when none is
// connected. TailScale and ZeroTier are always known; the remaining tunnels only
// report while the drawer has been opened at least once, so before that the icon
// reflects the two built-in ones only.
export function remoteIconColor(
  tailScaleState?: string,
  zeroTierState?: string,
  tunnelEntries?: Partial<Record<TunnelKey, TunnelEntry>>,
) {
  if (tunnelUp(tailScaleState) || tunnelUp(zeroTierState)) return GREEN;
  if (tunnelEntries && Object.values(tunnelEntries).some(entry => entry?.up)) return GREEN;
  return GREY;
}

interface TunnelRow {
  key: string;
  name: string;
  up: boolean;
  state?: string;
  detail?: string;
}

// RemoteStatusPanel is the drawer behind the Remote button. It lists every
// overlay network the device can run, in the order the hardware allows them to
// be configured: the two built-in ones first, then the installable tunnels.
export default function RemoteStatusPanel({
  tailScaleState,
  zeroTierState,
  active = true,
}: RemoteStatusPanelProps) {
  const { $at } = useReactAt();
  const tunnelEntries = useVpnStore(state => state.tunnelEntries);

  // On the desktop bar the panel content only mounts while the drawer is open.
  // The mobile drawer keeps its children mounted, so it passes `active` instead.
  useRemoteTunnels(active);

  const flagRow = (key: TunnelKey, name: string): TunnelRow => {
    const entry = tunnelEntries[key];
    return { key, name, up: Boolean(entry?.up), state: entry?.state, detail: entry?.detail };
  };

  const rows: TunnelRow[] = [
    { key: "tailscale", name: $at("TailScale"), up: tunnelUp(tailScaleState), state: tailScaleState },
    { key: "zerotier", name: $at("Zerotier"), up: tunnelUp(zeroTierState), state: zeroTierState },
    flagRow("netbird", $at("Netbird")),
    flagRow("wireguard", $at("WireGuard")),
    flagRow("easytier", $at("EasyTier")),
    flagRow("vnt", $at("VNT")),
    flagRow("frpc", $at("FRP")),
    flagRow("cloudflared", $at("Cloudflare Tunnel")),
  ];

  // The address is what people actually want once a tunnel is up; until then the
  // state label explains why it is not.
  const valueFor = (row: TunnelRow) => {
    if (row.up && row.detail) return row.detail;
    return $at(STATE_LABEL[row.state || ""] || "Unknown");
  };

  const upCount = rows.filter(row => row.up).length;
  const badgeText = upCount > 0 ? `${upCount} of ${rows.length} up` : "Off";

  return (
    <StatusPanel
      icon={
        <NetworkSVG
          fontSize={14}
          style={{ color: remoteIconColor(tailScaleState, zeroTierState, tunnelEntries) }}
        />
      }
      title={$at("Remote Access")}
      badge={{ text: badgeText, tone: upCount > 0 ? "ok" : "idle" }}
      minWidth={260}
    >
      {rows.map(row => (
        <StatusPanelRow
          key={row.key}
          dotColor={row.up ? GREEN : GREY}
          label={row.name}
          value={valueFor(row)}
          dim={!row.up}
        />
      ))}
    </StatusPanel>
  );
}

// RemoteStatusPanelConnected is the store-bound variant used by the mobile
// drawer, where there is no bottom bar button to pass the states down.
export function RemoteStatusPanelConnected() {
  const tailScaleState = useVpnStore(state => state.tailScaleConnectionState);
  const zeroTierState = useVpnStore(state => state.zeroTierConnectionState);
  const isOpen = useUiStore(state => state.sidebarView === "RemoteStatus");

  return (
    <RemoteStatusPanel
      tailScaleState={tailScaleState}
      zeroTierState={zeroTierState}
      active={isOpen}
    />
  );
}
