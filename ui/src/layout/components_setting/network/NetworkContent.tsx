import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import { LuEthernetPort, LuTrash2, LuPlus } from "react-icons/lu";
import { Button as AntdButton, Select , Input } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";
import { isMobile } from "react-device-detect";

import {
  IPv4Mode,
  IPv4StaticConfig,
  IPv6Mode,
  LLDPMode,
  mDNSMode,
  NetworkSettings,
  NetworkState,
  TimeSyncMode,
  useNetworkStateStore,
} from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { TimeSyncCustomServer } from "@/hooks/stores";
import { Button } from "@components/Button";
import { GridCard } from "@components/Card";
import { InputFieldWithLabel } from "@components/InputField";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import { SettingsPageHeader } from "@components/Settings/SettingsPageheader";
import Fieldset from "@components/Fieldset";
import { ConfirmDialog } from "@components/ConfirmDialog";
import notifications from "@/notifications";
import Ipv6NetworkCard from "@components/Network/Ipv6NetworkCard";
import EmptyCard from "@components/EmptyCard";
import AutoHeight from "@components/AutoHeight";
import DhcpLeaseCard from "@components/Network/DhcpLeaseCard";
import { SettingsItem } from "@components/Settings/SettingsView";
import { text_primary_color } from "@/layout/theme_color";

dayjs.extend(relativeTime);

const defaultNetworkSettings: NetworkSettings = {
  hostname: "",
  domain: "",
  http_proxy: "",
  https_proxy: "",
  all_proxy: "",
  ipv4_mode: "unknown",
  ipv6_mode: "unknown",
  lldp_mode: "unknown",
  lldp_tx_tlvs: [],
  mdns_mode: "unknown",
  time_sync_mode: "unknown",
};

const MAX_VISIBLE_SERVER_ROWS = 4;
const SERVER_ROW_HEIGHT_PX = 36;
const GREEN_ACTION_BUTTON_CLASSNAME =
  "!h-[36px] !text-[rgba(20,204,45,1)] !border-[rgba(20,204,45,1)]";

function TimeSyncServerBlock(props: {
  title: string;
  builtinOptions: string[];
  selected?: string[];
  customs?: TimeSyncCustomServer[];
  onToggleBuiltin: (server: string, checked: boolean) => void;
  onAddCustom: (server: string) => void;
  onToggleCustom: (index: number, enabled: boolean) => void;
  onRemoveCustom: (index: number) => void;
  onReset: () => void;
}) {
  const { $at } = useReactAt();
  const [customInput, setCustomInput] = useState("");
  const { title, builtinOptions, selected, customs } = props;

  // A missing (null/undefined) selection means "never configured -> use all built-in servers".
  // An empty selection means the user explicitly deselected every built-in server.
  // The backend marshals a nil slice as JSON null, so both null and undefined must be handled.
  const isBuiltinChecked = (server: string) => (selected == null ? true : selected.includes(server));
  const customCount = customs?.length ?? 0;
  const enabledCustomCount = (customs ?? []).filter(customServer => customServer.enabled && customServer.server).length;
  const checkedBuiltinCount = builtinOptions.filter(server => isBuiltinChecked(server)).length;
  const hasActiveServer = checkedBuiltinCount + enabledCustomCount > 0;
  const maxListHeight = (rowCount: number) =>
    rowCount > MAX_VISIBLE_SERVER_ROWS ? MAX_VISIBLE_SERVER_ROWS * SERVER_ROW_HEIGHT_PX : undefined;
  const rowBorder = "border-slate-800/10 dark:border-slate-300/20";
  const labelTextClassName = "text-sm text-slate-600 dark:text-[#ffffff]";

  return (
    <GridCard>
      <div className="animate-fadeIn p-4 text-black dark:text-white">
        <div className="space-y-3">
          <h3 className="text-base font-bold text-slate-900 dark:text-white">{title}</h3>

          <div>
            <div className={`mb-1 ${labelTextClassName}`}>{$at("Built-in servers")}</div>
            <div
              className={`overflow-y-auto rounded-sm border ${rowBorder}`}
              style={{ maxHeight: maxListHeight(builtinOptions.length) }}
            >
              {builtinOptions.map((server, index) => (
                <label
                  key={server}
                  className={`flex cursor-pointer items-center gap-x-2 px-3 py-2 ${
                    index > 0 ? `border-t ${rowBorder}` : ""
                  }`}
                >
                  <input
                    type="checkbox"
                    className="accent-blue-600"
                    checked={isBuiltinChecked(server)}
                    onChange={e => props.onToggleBuiltin(server, e.target.checked)}
                  />
                  <span className="text-sm font-medium">{server}</span>
                </label>
              ))}
            </div>
          </div>

          <div>
            <div className={`mb-1 ${labelTextClassName}`}>{$at("Custom servers")}</div>
            <div
              className={`overflow-y-auto rounded-sm border ${rowBorder}`}
              style={{ maxHeight: maxListHeight(customCount) }}
            >
              {customCount === 0 ? (
                <div className="px-3 py-2 text-sm italic text-slate-400 dark:text-slate-500">
                  {$at("No custom servers")}
                </div>
              ) : (
                customs!.map((customServer, index) => (
                  <div
                    key={`${customServer.server}-${index}`}
                    className={`flex items-center gap-x-2 px-3 py-2 ${
                      index > 0 ? `border-t ${rowBorder}` : ""
                    }`}
                  >
                    <label className="flex flex-1 cursor-pointer items-center gap-x-2">
                      <input
                        type="checkbox"
                        className="accent-blue-600"
                        checked={customServer.enabled}
                        onChange={e => props.onToggleCustom(index, e.target.checked)}
                      />
                      <span
                        className={`text-sm font-medium ${
                          customServer.enabled ? "" : "text-slate-400 line-through dark:text-slate-500"
                        }`}
                      >
                        {customServer.server}
                      </span>
                    </label>
                    <button
                      type="button"
                      title={$at("Delete")}
                      className="text-slate-400 transition-colors hover:text-red-500 dark:text-slate-500"
                      onClick={() => props.onRemoveCustom(index)}
                    >
                      <LuTrash2 size={16} />
                    </button>
                  </div>
                ))
              )}
            </div>
          </div>

          {!hasActiveServer && (
            <div className="rounded-sm border border-red-500/60 bg-red-50/60 px-3 py-2 text-sm text-red-600 dark:border-red-500/40 dark:bg-red-900/20 dark:text-red-400">
              {$at("No servers selected, time synchronization will fail")}
            </div>
          )}

          <div className="flex items-center gap-x-2">
            <Input
              type="text"
              value={customInput}
              onChange={e => setCustomInput(e.target.value)}
              onPressEnter={() => {
                const value = customInput.trim();
                if (!value) return;
                props.onAddCustom(value);
                setCustomInput("");
              }}
              placeholder={$at("Add custom server")}
              className="!h-[36px] flex-1"
            />
            <AntdButton
              type="primary"
              className="!h-[36px]"
              disabled={!customInput.trim()}
              icon={<LuPlus size={16} />}
              onClick={() => {
                const value = customInput.trim();
                if (!value) return;
                props.onAddCustom(value);
                setCustomInput("");
              }}
            >
              {$at("Add")}
            </AntdButton>
          </div>

          <div>
            <AntdButton className={GREEN_ACTION_BUTTON_CLASSNAME} onClick={props.onReset}>
              {$at("Restore built-in defaults")}
            </AntdButton>
          </div>
        </div>
      </div>
    </GridCard>
  );
}

export function LifeTimeLabel({ lifetime }: { lifetime: string }) {
  const [remaining, setRemaining] = useState<string | null>(null);

  useEffect(() => {
    setRemaining(dayjs(lifetime).fromNow());

    const interval = setInterval(() => {
      setRemaining(dayjs(lifetime).fromNow());
    }, 1000 * 30);
    return () => clearInterval(interval);
  }, [lifetime]);

  if (lifetime == "") {
    return <strong>N/A</strong>;
  }

  return (
    <>
      <span className="text-sm font-medium">{remaining && <> {remaining}</>}</span>
      <span className="text-xs text-slate-700 dark:text-slate-300">
        {" "}
        ({dayjs(lifetime).format("YYYY-MM-DD HH:mm")})
      </span>
    </>
  );
}

export default function SettingsNetwork() {
  const { $at } = useReactAt();
  const [send] = useJsonRpc();
  const [networkState, setNetworkState] = useNetworkStateStore(state => [
    state,
    state.setNetworkState,
  ]);

  const [networkSettings, setNetworkSettings] =
    useState<NetworkSettings>(defaultNetworkSettings);

  const [macAddressInput, setMacAddressInput] = useState<string>("");
  const macAddressTouched = useRef(false);
  const initialMacAddress = useRef<string>("");

  // We use this to determine whether the settings have changed
  const firstNetworkSettings = useRef<NetworkSettings | undefined>(undefined);
  // We use this to indicate whether saved settings differ from initial (effective) settings
  const initialNetworkSettings = useRef<NetworkSettings | undefined>(undefined);
  const [networkSettingsLoaded, setNetworkSettingsLoaded] = useState(false);
  const { id } = useParams();
  const baselineKey = id ? `network_baseline_${id}` : "network_baseline";
  const baselineResetKey = id ? `network_baseline_reset_${id}` : "network_baseline_reset";

  useEffect(() => {
    const url = new URL(window.location.href);
    if (url.searchParams.get("networkChanged") === "true") {
      localStorage.setItem(baselineResetKey, "1");
      url.searchParams.delete("networkChanged");
      window.history.replaceState(null, "", url.toString());
    }
  }, [baselineResetKey]);

  const [customDomain, setCustomDomain] = useState<string>("");
  const [selectedDomainOption, setSelectedDomainOption] = useState<string>("local");

  useEffect(() => {
    if (networkSettings.domain && networkSettingsLoaded) {
      // Check if the domain is one of the predefined options
      const predefinedOptions = ["dhcp", "local"];
      if (predefinedOptions.includes(networkSettings.domain)) {
        setSelectedDomainOption(networkSettings.domain);
      } else {
        setSelectedDomainOption("custom");
        setCustomDomain(networkSettings.domain);
      }
    }
  }, [networkSettings.domain, networkSettingsLoaded]);

  const getNetworkSettings = useCallback(() => {
    setNetworkSettingsLoaded(false);
    send("getNetworkSettings", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkSettings(resp.result as NetworkSettings);

      if (!firstNetworkSettings.current) {
        firstNetworkSettings.current = resp.result as NetworkSettings;
      }
      const resetFlag = localStorage.getItem(baselineResetKey);
      const stored = localStorage.getItem(baselineKey);
      if (resetFlag) {
        initialNetworkSettings.current = resp.result as NetworkSettings;
        localStorage.setItem(baselineKey, JSON.stringify(resp.result));
        localStorage.removeItem(baselineResetKey);
      } else if (stored) {
        try {
          const parsed = JSON.parse(stored) as NetworkSettings;
          const server = resp.result as NetworkSettings;
          if (JSON.stringify(parsed) !== JSON.stringify(server)) {
            initialNetworkSettings.current = server;
            localStorage.setItem(baselineKey, JSON.stringify(server));
          } else {
            initialNetworkSettings.current = parsed;
          }
        } catch {
          initialNetworkSettings.current = resp.result as NetworkSettings;
          localStorage.setItem(baselineKey, JSON.stringify(resp.result));
        }
      } else {
        initialNetworkSettings.current = resp.result as NetworkSettings;
        localStorage.setItem(baselineKey, JSON.stringify(resp.result));
      }
      setNetworkSettingsLoaded(true);
    });
  }, [send]);

  const getNetworkState = useCallback(() => {
    send("getNetworkState", {}, resp => {
      if ("error" in resp) return;
      console.log(resp.result);
      setNetworkState(resp.result as NetworkState);
    });
  }, [send, setNetworkState]);

  const normalizeMacAddress = useCallback((value: string) => {
    return value.trim().toLowerCase();
  }, []);

  const isValidMacAddress = useCallback((value: string) => {
    const v = normalizeMacAddress(value);
    return /^([0-9a-f]{2}:){5}[0-9a-f]{2}$/.test(v);
  }, [normalizeMacAddress]);

  const setNetworkSettingsRemote = useCallback(
    (settings: NetworkSettings) => {
      const currentMac = (networkState?.mac_address || "").toLowerCase();
      const newMac = normalizeMacAddress(macAddressInput);
      const macChanged = newMac !== currentMac;

      if (macChanged) {
        if (!isValidMacAddress(macAddressInput)) {
          notifications.error("Please enter a valid MAC address");
          return;
        }
        setPendingMacAddress(newMac);
        setShowMacChangeConfirm(true);
      } else {
        saveNetworkSettings(settings);
      }
    },
    [networkState?.mac_address, macAddressInput, isValidMacAddress, normalizeMacAddress],
  );

  const saveNetworkSettings = useCallback(
    (settings: NetworkSettings, onSaved?: () => void) => {
      setNetworkSettingsLoaded(false);
      send("setNetworkSettings", { settings }, resp => {
        if ("error" in resp) {
          notifications.error(
            "Failed to save network settings: " +
            (resp.error.data ? resp.error.data : resp.error.message),
          );
          setNetworkSettingsLoaded(true);
          return;
        }
        firstNetworkSettings.current = resp.result as NetworkSettings;
        setNetworkSettings(resp.result as NetworkSettings);
        macAddressTouched.current = false;
        getNetworkState();
        setNetworkSettingsLoaded(true);
        notifications.success("Network settings saved");
        onSaved?.();
      });
    },
    [getNetworkState, send],
  );

  const handleRenewLease = useCallback(() => {
    send("renewDHCPLease", {}, resp => {
      if ("error" in resp) {
        notifications.error("Failed to renew lease: " + resp.error.message);
      } else {
        notifications.success("DHCP lease renewed");
      }
    });
  }, [send]);

  const [timeSyncServerOptions, setTimeSyncServerOptions] = useState<{ ntp: string[]; http: string[] }>({ ntp: [], http: [] });

  const getTimeSyncServers = useCallback(() => {
    send("getTimeSyncServers", {}, resp => {
      if ("error" in resp) return;
      setTimeSyncServerOptions(resp.result as { ntp: string[]; http: string[] });
    });
  }, [send]);

  useEffect(() => {
    getNetworkState();
    getNetworkSettings();
    getTimeSyncServers();
  }, [getNetworkState, getNetworkSettings, getTimeSyncServers]);

  useEffect(() => {
    if (networkState?.mac_address && initialMacAddress.current === "") {
      const normalized = networkState.mac_address.toLowerCase();
      setMacAddressInput(normalized);
      initialMacAddress.current = normalized;
    }
  }, [networkState?.mac_address]);

  const handleIpv4ModeChange = (value: IPv4Mode | string) => {
    const newMode = value as IPv4Mode;
    const updatedSettings: NetworkSettings = { ...networkSettings, ipv4_mode: newMode };

    // Initialize static config if switching to static mode
    if (newMode === "static" && !updatedSettings.ipv4_static) {
      updatedSettings.ipv4_static = {
        address: "",
        netmask: "",
        gateway: "",
        dns: [],
      };
    }
    
    setNetworkSettings(updatedSettings);
  };

  const handleIpv4RequestAddressChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, ipv4_request_address: value });
  };

  const handleIpv4StaticChange = (field: keyof IPv4StaticConfig, value: string | string[]) => {
    const staticConfig = networkSettings.ipv4_static || {
      address: "",
      netmask: "",
      gateway: "",
      dns: [],
    };
    
    setNetworkSettings({
      ...networkSettings,
      ipv4_static: {
        ...staticConfig,
        [field]: value,
      },
    });
  };

  const handleIpv6ModeChange = (value: IPv6Mode | string) => {
    setNetworkSettings({ ...networkSettings, ipv6_mode: value as IPv6Mode });
  };

  const handleLldpModeChange = (value: LLDPMode | string) => {
    setNetworkSettings({ ...networkSettings, lldp_mode: value as LLDPMode });
  };

  const handleMdnsModeChange = (value: mDNSMode | string) => {
    setNetworkSettings({ ...networkSettings, mdns_mode: value as mDNSMode });
  };

  const handleTimeSyncModeChange = (value: TimeSyncMode | string) => {
    setNetworkSettings({ ...networkSettings, time_sync_mode: value as TimeSyncMode });
  };

  const handleHostnameChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, hostname: value });
  };

  const handleDomainChange = (value: string) => {
    setNetworkSettings({ ...networkSettings, domain: value });
  };

  const handleProxyChange = (field: "http_proxy" | "https_proxy" | "all_proxy", value: string) => {
    setNetworkSettings({ ...networkSettings, [field]: value });
  };

  const handleDomainOptionChange = (value: string) => {
    setSelectedDomainOption(value);
    if (value !== "custom") {
      handleDomainChange(value);
    }
  };

  const handleCustomDomainChange = (value: string) => {
    setCustomDomain(value);
    handleDomainChange(value);
  };

  const filterUnknown = useCallback(
    (options: { value: string; label: string }[]) => {
      if (!networkSettingsLoaded) return options;
      return options.filter(option => option.value !== "unknown");
    },
    [networkSettingsLoaded],
  );

  const [showRenewLeaseConfirm, setShowRenewLeaseConfirm] = useState(false);
  const [applyingRequestAddr, setApplyingRequestAddr] = useState(false);
  const [showRequestAddrConfirm, setShowRequestAddrConfirm] = useState(false);
  const [showApplyStaticConfirm, setShowApplyStaticConfirm] = useState(false);
  const [showIpv4RestartConfirm, setShowIpv4RestartConfirm] = useState(false);
  const [showMacChangeConfirm, setShowMacChangeConfirm] = useState(false);
  const [pendingMacAddress, setPendingMacAddress] = useState("");
  const [pendingIpv4Mode, setPendingIpv4Mode] = useState<IPv4Mode | null>(null);
  const [ipv4StaticDnsText, setIpv4StaticDnsText] = useState("");

  const handleApplyRequestAddress = useCallback(() => {
    const requested = (networkSettings.ipv4_request_address || "").trim();
    if (!requested) {
      notifications.error("Please enter a valid Request Address");
      return;
    }
    if (networkSettings.ipv4_mode !== "dhcp") {
      notifications.error("Request Address is only available in DHCP mode");
      return;
    }
    setApplyingRequestAddr(true);
    send("setNetworkSettings", { settings: networkSettings }, resp => {
      if ("error" in resp) {
        setApplyingRequestAddr(false);
        return notifications.error(
          "Failed to save Request Address: " + (resp.error.data ? resp.error.data : resp.error.message),
        );
      }
      setApplyingRequestAddr(false);
      notifications.success("Request Address saved. Changes will take effect after restart.");
    });
  }, [networkSettings, send]);

  useEffect(() => {
    const dns = (networkSettings.ipv4_static?.dns || []).join(", ");
    setIpv4StaticDnsText(dns);
  }, [networkSettings.ipv4_static?.dns]);
//
  return (
    <>
      <Fieldset disabled={!networkSettingsLoaded} className="space-y-4 pb-[50px]">
        <SettingsPageHeader
          title={$at("Network")}
          description={$at("Configure your network settings")}
        />
        <div className="space-y-4">
          <SettingsItem
            title={$at("MAC Address")}
            description={$at("Hardware identifier for the network interface")}
            className={`${isMobile ? "w-full flex-col" : ""}`}
          >
            <Input
              type="text"
              value={macAddressInput}
              onChange={e => {
                macAddressTouched.current = true;
                setMacAddressInput(e.target.value);
              }}
              className={isMobile ? "!w-full !h-[36px]" : "!w-[35%] !h-[36px]"}
            />
          </SettingsItem>
        </div>
        <div className="space-y-4">
          <SettingsItem
            title="Hostname"
            description={$at("Device identifier on the network. Blank for system default")}
            className={`${isMobile ? "w-full flex-col" : ""}`}
          >
            <div className={`relative ${isMobile ? "w-full" : "w-[37%]"} `}>
              <Input
                type="text"
                value={networkSettings.hostname}
                className={isMobile ? "!w-full !h-[36px]" : "!w-full !h-[36px]"}
                onChange={e => {
                  handleHostnameChange(e.target.value);
                }}
              />
              {/*<InputField*/}
              {/*    size="SM"*/}
              {/*    type="text"*/}
              {/*    value={networkSettings.hostname}*/}
              {/*    placeholder={networkSettings.hostname}*/}
              {/*    defaultValue={networkSettings.hostname}*/}
              {/*    onChange={e => {*/}
              {/*      handleHostnameChange(e.target.value);*/}
              {/*    }}*/}
              {/*  />*/}

            </div>
          </SettingsItem>
        </div>

        <div className="space-y-4">
          <div className="space-y-1">
            <SettingsItem
              title={$at("Domain")}
              description={$at("Device domain suffix in mDNS network")}
              className={`${isMobile ? "w-full flex-col" : ""}`}
            >
              <div className={`space-y-2 ${isMobile ? "w-full" : "w-[37%]"}`}>
                <Select
                  className={`!w-full !h-[36px]`}
                  value={selectedDomainOption}
                  onChange={e => handleDomainOptionChange(e)}
                  options={[
                    { value: "dhcp", label: "DHCP provided" },
                    { value: "local", label: ".local" },
                    { value: "custom", label: "Custom" },
                  ]} />
                {/*<SelectMenuBasic*/}
                {/*  size="SM"*/}
                {/*  value={selectedDomainOption}*/}
                {/*  onChange={e => handleDomainOptionChange(e.target.value)}*/}
                {/*  options={[*/}
                {/*    { value: "dhcp", label: "DHCP provided" },*/}
                {/*    { value: "local", label: ".local" },*/}
                {/*    { value: "custom", label: "Custom" },*/}
                {/*  ]}*/}
                {/*/>*/}
              </div>
            </SettingsItem>
            {selectedDomainOption === "custom" && (
              <div className="mt-2 w-1/3 border-l border-slate-800/10 pl-4 dark:border-slate-300/20">
                <InputFieldWithLabel
                  size="SM"
                  type="text"
                  label={$at("Custom Domain")}
                  placeholder="home"
                  value={customDomain}
                  onChange={e => {
                    setCustomDomain(e.target.value);
                    handleCustomDomainChange(e.target.value);
                  }}
                />
              </div>
            )}
          </div>
          <div className="space-y-4">
            <SettingsItem
              title={$at("mDNS")}
              description={$at("Control mDNS (multicast DNS) operational mode")}
              className={`${isMobile ? "w-full flex-col" : ""}`}
            >
              <Select
                value={networkSettings.mdns_mode}
                className={isMobile ? "!w-full !h-[36px]" : "!w-[28%] !h-[36px]"}
                onChange={e => handleMdnsModeChange(e)}
                options={[
                  { value: "disabled", label: "Disabled" },
                  { value: "auto", label: "Auto" },
                  { value: "ipv4_only", label: "IPv4 only" },
                  { value: "ipv6_only", label: "IPv6 only" },
                ]}
              />

              {/*  <SelectMenuBasic*/}
              {/*    size="SM"*/}
              {/*    value={networkSettings.mdns_mode}*/}
              {/*    className={`${isMobile?"w-full":""}`}*/}
              {/*    onChange={e => handleMdnsModeChange(e.target.value)}*/}
              {/*    options={filterUnknown([*/}
              {/*      { value: "disabled", label: "Disabled" },*/}
              {/*      { value: "auto", label: "Auto" },*/}
              {/*      { value: "ipv4_only", label: "IPv4 only" },*/}
              {/*      { value: "ipv6_only", label: "IPv6 only" },*/}
              {/*    ])}*/}
              {/*  />*/}
              </SettingsItem>
          </div>

          <div className="space-y-4">
            <SettingsItem
              title={$at("Time synchronization")}
              description={$at("Configure time synchronization settings")}
              className={`${isMobile ? "w-full flex-col" : ""}`}

            >
              <Select
                className={isMobile ? "!w-full !h-[36px]" : "!w-[28%] !h-[36px]"}
                value={networkSettings.time_sync_mode}
                onChange={e => {
                  handleTimeSyncModeChange(e);
                }}
                options={filterUnknown([
                  { value: "unknown", label: "..." },
                  // { value: "auto", label: $at("Auto") },
                  { value: "ntp_only", label: $at("NTP only") },
                  { value: "ntp_and_http", label: $at("NTP and HTTP") },
                  { value: "http_only", label: $at("HTTP only") },
                  { value: "disabled", label: $at("Disabled (use system time)") },
                  // { value: "custom", label: "Custom" },
                ])}/>
              {/*<SelectMenuBasic*/}
              {/*  size="SM"*/}
              {/*  value={networkSettings.time_sync_mode}*/}
              {/*  className={`${isMobile ? "w-full" : ""}`}*/}
              {/*  onChange={e => {*/}
              {/*    handleTimeSyncModeChange(e.target.value);*/}
              {/*  }}*/}
              {/*  options={filterUnknown([*/}
              {/*    { value: "unknown", label: "..." },*/}
              {/*    // { value: "auto", label: "Auto" },*/}
              {/*    { value: "ntp_only", label: "NTP only" },*/}
              {/*    { value: "ntp_and_http", label: "NTP and HTTP" },*/}
              {/*    { value: "http_only", label: "HTTP only" },*/}
              {/*    // { value: "custom", label: "Custom" },*/}
              {/*  ])}*/}
              {/*/>*/}
            </SettingsItem>

            {(networkSettings.time_sync_mode === "ntp_only" ||
              networkSettings.time_sync_mode === "ntp_and_http") && (
              <TimeSyncServerBlock
                title={$at("NTP Servers")}
                builtinOptions={timeSyncServerOptions.ntp}
                selected={networkSettings.time_sync_ntp_servers}
                customs={networkSettings.time_sync_ntp_custom}
                onToggleBuiltin={(server, checked) => {
                  const all = timeSyncServerOptions.ntp;
                  // null/undefined means "all built-ins"; an empty array stays empty
                  const current =
                    networkSettings.time_sync_ntp_servers == null ? [...all] : [...networkSettings.time_sync_ntp_servers];
                  const next = checked
                    ? [...current, server]
                    : current.filter(s => s !== server);
                  // re-checking everything restores the "use all built-ins" default;
                  // an empty list is kept as-is so "none selected" survives a reload
                  const value = next.length === all.length ? undefined : next;
                  setNetworkSettings({ ...networkSettings, time_sync_ntp_servers: value });
                }}
                onAddCustom={server => {
                  const customs = [...(networkSettings.time_sync_ntp_custom ?? []), { server, enabled: true }];
                  setNetworkSettings({ ...networkSettings, time_sync_ntp_custom: customs });
                }}
                onToggleCustom={(index, enabled) => {
                  const customs = (networkSettings.time_sync_ntp_custom ?? []).map((c, i) =>
                    i === index ? { ...c, enabled } : c,
                  );
                  setNetworkSettings({ ...networkSettings, time_sync_ntp_custom: customs });
                }}
                onRemoveCustom={index => {
                  const customs = (networkSettings.time_sync_ntp_custom ?? []).filter((_, i) => i !== index);
                  setNetworkSettings({ ...networkSettings, time_sync_ntp_custom: customs.length ? customs : undefined });
                }}
                onReset={() => {
                  setNetworkSettings({
                    ...networkSettings,
                    time_sync_ntp_servers: undefined,
                    time_sync_ntp_custom: undefined,
                  });
                }}
              />
            )}

            {(networkSettings.time_sync_mode === "http_only" ||
              networkSettings.time_sync_mode === "ntp_and_http") && (
              <TimeSyncServerBlock
                title={$at("HTTP Servers")}
                builtinOptions={timeSyncServerOptions.http}
                selected={networkSettings.time_sync_http_urls}
                customs={networkSettings.time_sync_http_custom}
                onToggleBuiltin={(server, checked) => {
                  const all = timeSyncServerOptions.http;
                  const current =
                    networkSettings.time_sync_http_urls == null ? [...all] : [...networkSettings.time_sync_http_urls];
                  const next = checked
                    ? [...current, server]
                    : current.filter(s => s !== server);
                  const value = next.length === all.length ? undefined : next;
                  setNetworkSettings({ ...networkSettings, time_sync_http_urls: value });
                }}
                onAddCustom={server => {
                  const customs = [...(networkSettings.time_sync_http_custom ?? []), { server, enabled: true }];
                  setNetworkSettings({ ...networkSettings, time_sync_http_custom: customs });
                }}
                onToggleCustom={(index, enabled) => {
                  const customs = (networkSettings.time_sync_http_custom ?? []).map((c, i) =>
                    i === index ? { ...c, enabled } : c,
                  );
                  setNetworkSettings({ ...networkSettings, time_sync_http_custom: customs });
                }}
                onRemoveCustom={index => {
                  const customs = (networkSettings.time_sync_http_custom ?? []).filter((_, i) => i !== index);
                  setNetworkSettings({ ...networkSettings, time_sync_http_custom: customs.length ? customs : undefined });
                }}
                onReset={() => {
                  setNetworkSettings({
                    ...networkSettings,
                    time_sync_http_urls: undefined,
                    time_sync_http_custom: undefined,
                  });
                }}
              />
            )}
          </div>

          <div className="space-y-4">
          <SettingsItem
            title={$at("HTTP Proxy")}
            description={$at("Configure program HTTP proxy (optional)")}
            className={`${isMobile ? "w-full flex-col" : ""}`}
          >
            <Input
              type="text"
              value={networkSettings.http_proxy || ""}
              placeholder="http://127.0.0.1:7890"
              onChange={e => handleProxyChange("http_proxy", e.target.value)}
              className={isMobile ? "!w-full !h-[36px]" : "!w-[37%] !h-[36px]"}
            />
          </SettingsItem>
          <SettingsItem
            title={$at("HTTPS Proxy")}
            description={$at("Configure program HTTPS proxy (optional)")}
            className={`${isMobile ? "w-full flex-col" : ""}`}
          >
            <Input
              type="text"
              value={networkSettings.https_proxy || ""}
              placeholder="http://127.0.0.1:7890"
              onChange={e => handleProxyChange("https_proxy", e.target.value)}
              className={isMobile ? "!w-full !h-[36px]" : "!w-[37%] !h-[36px]"}
            />
          </SettingsItem>
          <SettingsItem
            title={$at("ALL Proxy")}
            description={$at("Configure program ALL proxy (optional)")}
            className={`${isMobile ? "w-full flex-col" : ""}`}
          >
            <Input
              type="text"
              value={networkSettings.all_proxy || ""}
              placeholder="socks5://127.0.0.1:7890"
              onChange={e => handleProxyChange("all_proxy", e.target.value)}
              className={isMobile ? "!w-full !h-[36px]" : "!w-[37%] !h-[36px]"}
            />
          </SettingsItem>
          </div>
          <AntdButton
            type="primary"
            disabled={
              (!macAddressTouched.current && firstNetworkSettings.current === networkSettings) ||
              (networkSettings.ipv4_mode === "static" && firstNetworkSettings.current?.ipv4_mode !== "static") ||
              (macAddressTouched.current && !isValidMacAddress(macAddressInput))
            }
            onClick={() => setNetworkSettingsRemote(networkSettings)}
            className={isMobile ? "w-full" : ""}
          >
            {$at("Save settings")}
          </AntdButton>
        </div>

        <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

        <div className="space-y-4">
          <SettingsItem title={$at("IPv4 Mode")} description={$at("Configure IPv4 mode")}>
            <Select
              className={isMobile ? "!w-full !h-[36px]" : "!w-[28%] !h-[36px]"}
              value={networkSettings.ipv4_mode}
              onChange={e => handleIpv4ModeChange(e)}
              options={filterUnknown([
                { value: "dhcp", label: "DHCP" },
                { value: "static", label: "Static" },
              ])}
            />

            {/*<SelectMenuBasic*/}
            {/*  size="SM"*/}
            {/*  value={networkSettings.ipv4_mode}*/}
            {/*  className={`${isMobile ? "w-full" : ""}`}*/}
            {/*  onChange={e => handleIpv4ModeChange(e.target.value)}*/}
            {/*  options={filterUnknown([*/}
            {/*    { value: "dhcp", label: "DHCP" },*/}
            {/*    // { value: "static", label: "Static" },*/}
            {/*  ])}*/}
            {/*/>*/}
          </SettingsItem>
          {networkSettings.ipv4_mode === "dhcp" && (   
            <div className="flex items-end gap-x-2">
              <InputFieldWithLabel
                size="SM"
                type="text"
                label={$at("Request Address")}
                placeholder="192.168.1.100"
                value={networkSettings.ipv4_request_address || ""}
                onChange={e => {
                  handleIpv4RequestAddressChange(e.target.value);
                }}
              />
              <AntdButton
                type="primary"
                disabled={applyingRequestAddr || !networkSettings.ipv4_request_address}
                onClick={() => setNetworkSettingsRemote(networkSettings)}
                className={"!h-[38px]"}
              >{$at("Apply")}</AntdButton>
            </div>
          )}

          {networkSettings.ipv4_mode === "static" && (
            <AutoHeight>
              <GridCard>
                <div className="p-4 mt-1 space-y-4 border-l border-slate-800/10 pl-4 dark:border-slate-300/20 items-end gap-x-2">
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label={$at("IP Address")}
                    placeholder="192.168.1.100"
                    value={networkSettings.ipv4_static?.address || ""}
                    onChange={e => {
                      handleIpv4StaticChange("address", e.target.value);
                    }}
                  />
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label={$at("Netmask")}
                    placeholder="255.255.255.0"
                    value={networkSettings.ipv4_static?.netmask || ""}
                    onChange={e => {
                      handleIpv4StaticChange("netmask", e.target.value);
                    }}
                  />
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label={$at("Gateway")}
                    placeholder="192.168.1.1"
                    value={networkSettings.ipv4_static?.gateway || ""}
                    onChange={e => {
                      handleIpv4StaticChange("gateway", e.target.value);
                    }}
                  />
                  <InputFieldWithLabel
                    size="SM"
                    type="text"
                    label={$at("DNS Servers")}
                    placeholder="8.8.8.8,8.8.4.4"
                    value={ipv4StaticDnsText}
                    onChange={e => setIpv4StaticDnsText(e.target.value)}
                  />

                  <div className="flex items-center gap-x-2">
                    <Button
                      size="SM"
                      theme="primary"
                      text={$at("Save")}
                      onClick={() => setShowApplyStaticConfirm(true)}
                    />
                  </div> 
                </div> 
              </GridCard>
            </AutoHeight>
          )}

          {networkSettings.ipv4_mode === "dhcp" && (
          <AutoHeight>
            {!networkSettingsLoaded && !networkState?.dhcp_lease ? (
              <GridCard>
                <div className="p-4">
                  <div className="space-y-4">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                      {$at("DHCP Lease Information")}
                    </h3>
                    <div className="animate-pulse space-y-3">
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                    </div>
                  </div>
                </div>
              </GridCard>
            ) : networkState?.dhcp_lease && networkState.dhcp_lease.ip ? (
              <DhcpLeaseCard
                networkState={networkState}
                setShowRenewLeaseConfirm={setShowRenewLeaseConfirm}
              />
            ) : (

              <EmptyCard
                IconElm={LuEthernetPort}
                headline={$at("DHCP Information")}
                description={$at("No DHCP lease information available")}
              />
            )}
          </AutoHeight>
          )}
        </div>
        <div className="space-y-4">
          <SettingsItem title={$at("IPv6 Mode")} description={$at("Configure the IPv6 mode")}>
            <Select
              className={isMobile ? "!w-full !h-[36px]" : "!w-[28%] !h-[36px]"}
              value={networkSettings.ipv6_mode}

              onChange={e => handleIpv6ModeChange(e)}
              options={filterUnknown([
                // { value: "disabled", label: "Disabled" },
                { value: "slaac", label: "SLAAC" },
                // { value: "dhcpv6", label: "DHCPv6" },
                // { value: "slaac_and_dhcpv6", label: "SLAAC and DHCPv6" },
                // { value: "static", label: "Static" },
                // { value: "link_local", label: "Link-local only" },
              ])}
            />

            {/*<SelectMenuBasic*/}
            {/*  size="SM"*/}
            {/*  value={networkSettings.ipv6_mode}*/}
            {/*  className={`${isMobile ? "w-full" : ""}`}*/}
            {/*  onChange={e => handleIpv6ModeChange(e.target.value)}*/}
            {/*  options={filterUnknown([*/}
            {/*    // { value: "disabled", label: "Disabled" },*/}
            {/*    { value: "slaac", label: "SLAAC" },*/}
            {/*    // { value: "dhcpv6", label: "DHCPv6" },*/}
            {/*    // { value: "slaac_and_dhcpv6", label: "SLAAC and DHCPv6" },*/}
            {/*    // { value: "static", label: "Static" },*/}
            {/*    // { value: "link_local", label: "Link-local only" },*/}
            {/*  ])}*/}
            {/*/>*/}
          </SettingsItem>
          <AutoHeight>
            {!networkSettingsLoaded &&
            !(networkState?.ipv6_addresses && networkState.ipv6_addresses.length > 0) ? (
              <GridCard>
                <div className="p-4">
                  <div className="space-y-4">
                    <h3 className="text-base font-bold text-slate-900 dark:text-white">
                      IPv6 Information
                    </h3>
                    <div className="animate-pulse space-y-3">
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/2 rounded bg-slate-200 dark:bg-slate-700" />
                      <div className="h-4 w-1/3 rounded bg-slate-200 dark:bg-slate-700" />
                    </div>
                  </div>
                </div>
              </GridCard>
            ) : networkState?.ipv6_addresses && networkState.ipv6_addresses.length > 0 ? (
              <Ipv6NetworkCard networkState={networkState} />
            ) : (
              <EmptyCard
                IconElm={LuEthernetPort}
                iconClassName={text_primary_color}
                headline={$at("IPv6 Information")}
                description={$at("No IPv6 addresses configured")}
              />
            )}
          </AutoHeight>
        </div>
        <div className="hidden space-y-4">
          <SettingsItem
            title="LLDP"
            description="Control which TLVs will be sent over Link Layer Discovery Protocol"
          >
            <SelectMenuBasic
              size="SM"
              value={networkSettings.lldp_mode}
              onChange={e => handleLldpModeChange(e.target.value)}
              options={filterUnknown([
                { value: "disabled", label: "Disabled" },
                { value: "basic", label: "Basic" },
                { value: "all", label: "All" },
              ])}
            />
          </SettingsItem>
        </div>
      </Fieldset>
      <ConfirmDialog
        open={showRenewLeaseConfirm}
        onClose={() => setShowRenewLeaseConfirm(false)}
        title={$at("Renew DHCP Lease")}
        description={$at("Changes will take effect after a restart.")}
        variant="danger"
        confirmText={$at("Renew DHCP Lease")}
        cancelText={$at("Cancel")}
        onConfirm={() => {
          handleRenewLease();
          setShowRenewLeaseConfirm(false);
        }}
      />
      <ConfirmDialog
        open={showApplyStaticConfirm}
        onClose={() => setShowApplyStaticConfirm(false)} 
        title={$at("Save Static IPv4 Settings?")}
        description={$at("Changes will take effect after a restart.")}
        variant="warning"
        confirmText={$at("Confirm")}
        cancelText={$at("Cancel")}
        onConfirm={() => {
          setShowApplyStaticConfirm(false);
          const dnsArray = ipv4StaticDnsText
            .split(",")
            .map(d => d.trim())
            .filter(d => d.length > 0);
          const updatedSettings: NetworkSettings = {
            ...networkSettings,
            ipv4_static: {
              ...(networkSettings.ipv4_static || { address: "", netmask: "", gateway: "", dns: [] }),
              dns: dnsArray,
            },
          };
          setNetworkSettingsRemote(updatedSettings);
        }}
      />
      <ConfirmDialog
        open={showRequestAddrConfirm}
        onClose={() => setShowRequestAddrConfirm(false)}
        title={$at("Save Request Address?")}
        description={$at("This will save the requested IPv4 address. Changes take effect after a restart.")}
        variant="warning"
        confirmText={$at("Save")}
        cancelText={$at("Cancel")}
        onConfirm={() => {
          setShowRequestAddrConfirm(false);
          handleApplyRequestAddress();
        }}
      />
      <ConfirmDialog
        open={showIpv4RestartConfirm}
        onClose={() => setShowIpv4RestartConfirm(false)}
        title={$at("Change IPv4 Mode?")}
        description={$at("IPv4 mode changes will take effect after a restart.")}
        variant="warning"
        confirmText={$at("Confirm")}
        cancelText={$at("Cancel")}
        onConfirm={() => {
          setShowIpv4RestartConfirm(false);
          if (pendingIpv4Mode) {
            const updatedSettings: NetworkSettings = { ...networkSettings, ipv4_mode: pendingIpv4Mode };
            if (pendingIpv4Mode === "static" && !updatedSettings.ipv4_static) {
              updatedSettings.ipv4_static = { address: "", netmask: "", gateway: "", dns: [] };
            }
            setNetworkSettings(updatedSettings);
            setNetworkSettingsRemote(updatedSettings);
            setPendingIpv4Mode(null);
          }
        }}
      />
      <ConfirmDialog
        open={showMacChangeConfirm}
        onClose={() => setShowMacChangeConfirm(false)}
        title={$at("Change MAC Address?")}
        description={$at("Changing the MAC address may cause the device IP to be reassigned and changed.")}
        variant="warning"
        confirmText={$at("Confirm")}
        cancelText={$at("Cancel")}
        onConfirm={() => {
          setShowMacChangeConfirm(false);
          send("setEthernetMacAddress", { macAddress: pendingMacAddress }, resp => {
            if ("error" in resp) {
              notifications.error(
                "Failed to apply MAC address: " +
                (resp.error.data ? resp.error.data : resp.error.message),
              );
              return;
            }
            setNetworkState(resp.result as NetworkState);
            saveNetworkSettings(networkSettings, () => {
              initialMacAddress.current = pendingMacAddress;
            });
          });
        }}
      />
    </>
);
}
