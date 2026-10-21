import { useCallback, useEffect, useState } from "react";
import { Button as AntdButton , Checkbox } from "antd";
import {useReactAt} from 'i18n-auto-extractor/react'
import { isMobile } from "react-device-detect";

import { ConfirmDialog } from "@components/ConfirmDialog";
import { SettingsPageHeader } from "@components/Settings/SettingsPageheader";
import { TextAreaWithLabel } from "@components/TextArea";
import { useSettingsStore, useHidStore } from "@/hooks/stores";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import api from "@/api";
import { isOnDevice } from "@/main";
import notifications from "@/notifications";
import { SettingsItem } from "@components/Settings/SettingsView";

export default function SettingsAdvanced() {
  const { $at }= useReactAt();
  const [send] = useJsonRpc();

  const [sshKey, setSSHKey] = useState<string>("");
  const setDeveloperMode = useSettingsStore(state => state.setDeveloperMode);
  const [usbEmulationEnabled, setUsbEmulationEnabled] = useState(false);
  const [usbEnhancedDetectionEnabled, setUsbEnhancedDetectionEnabled] =
    useState(true);
  const [showLoopbackWarning, setShowLoopbackWarning] = useState(false);
  const [showRebootConfirm, setShowRebootConfirm] = useState(false);
  const [showConfigEdit, setShowConfigEdit] = useState(false);
  const [showConfigSavedReboot, setShowConfigSavedReboot] = useState(false);
  const [configContent, setConfigContent] = useState("");
  const [isSavingConfig, setIsSavingConfig] = useState(false);
  const [localLoopbackOnly, setLocalLoopbackOnly] = useState(false);
  const [apiKey, setApiKey] = useState<string>("");
  const [apiKeyInput, setApiKeyInput] = useState<string>("");
  const [mcpServerEnabled, setMcpServerEnabled] = useState(true);
  const [mcpListening, setMcpListening] = useState(false);
  const [devModeEnabled, setDevModeEnabled] = useState(false);
  const [showApiKeyClearWarning, setShowApiKeyClearWarning] = useState(false);

  const settings = useSettingsStore();
  const isReinitializingGadget = useHidStore(state => state.isReinitializingGadget);
  const setIsReinitializingGadget = useHidStore(state => state.setIsReinitializingGadget);

  useEffect(() => {
    send("getSSHKeyState", {}, resp => {
      if ("error" in resp) return;
      setSSHKey(resp.result as string);
    });

    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });

    send("getUsbEnhancedDetection", {}, resp => {
      if ("error" in resp) return;
      setUsbEnhancedDetectionEnabled(resp.result as boolean);
    });

    send("getLocalLoopbackOnly", {}, resp => {
      if ("error" in resp) return;
      setLocalLoopbackOnly(resp.result as boolean);
    });

    send("getApiKey", {}, resp => {
      if ("error" in resp) return;
      const key = resp.result as string;
      setApiKey(key);
      setApiKeyInput(key);
    });

    send("getDevModeState", {}, resp => {
      if ("error" in resp) return;
      const state = resp.result as { enabled: boolean };
      setDevModeEnabled(Boolean(state?.enabled));
      setDeveloperMode(Boolean(state?.enabled));
    });
  }, [send, setDeveloperMode]);

  const getUsbEmulationState = useCallback(() => {
    send("getUsbEmulationState", {}, resp => {
      if ("error" in resp) return;
      setUsbEmulationEnabled(resp.result as boolean);
    });
  }, [send]);

  const handleUsbEmulationToggle = useCallback(
    (enabled: boolean) => {
      send("setUsbEmulationState", { enabled: enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} USB emulation: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setUsbEmulationEnabled(enabled);
        getUsbEmulationState();
      });
    },
    [getUsbEmulationState, send],
  );

  const handleUsbEnhancedDetectionToggle = useCallback(
    (enabled: boolean) => {
      send("setUsbEnhancedDetection", { enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} USB enhanced detection: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setUsbEnhancedDetectionEnabled(enabled);
        notifications.success(
          enabled ? "USB enhanced detection enabled" : "USB enhanced detection disabled",
        );
      });
    },
    [send],
  );

  const handleDevModeToggle = useCallback(
    (enabled: boolean) => {
      send("setDevModeState", { enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} developer mode: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setDevModeEnabled(enabled);
        setDeveloperMode(enabled);
        notifications.success(
          enabled
            ? "Developer mode enabled: /debug/pprof and /metrics are now reachable"
            : "Developer mode disabled: /debug/pprof and /metrics now return 404",
        );
      });
    },
    [send, setDeveloperMode],
  );

  const handleResetConfig = useCallback(() => {
    send("resetConfig", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to reset configuration: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Configuration reset to default successfully");
    });
  }, [send]);

  const handleUpdateApiKey = useCallback(() => {
    if (apiKeyInput === "") {
      setShowApiKeyClearWarning(true);
      return;
    }
    send("setApiKey", { apiKey: apiKeyInput }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update API key: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setApiKey(apiKeyInput);
      notifications.success("API key updated successfully");
    });
  }, [send, apiKeyInput]);

  // MCP server state lives in the management API, not in the JSON-RPC surface
  useEffect(() => {
    let cancelled = false;
    api
      .GET("/mcp/config")
      .then(res => (res.ok ? res.json() : null))
      .then(data => {
        if (!cancelled && data) {
          setMcpServerEnabled(Boolean(data.enabled));
          setMcpListening(Boolean(data.listening));
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  const handleMcpServerToggle = useCallback((enabled: boolean) => {
    api
      .POST("/mcp/config", { enabled })
      .then(async res => {
        const data = await res.json().catch(() => ({}));
        if (!res.ok) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} MCP server: ${data?.error || "Unknown error"}`,
          );
          return;
        }
        setMcpServerEnabled(Boolean(data.enabled));
        setMcpListening(Boolean(data.listening));
        notifications.success(
          enabled ? "MCP server enabled" : "MCP server disabled",
        );
      })
      .catch(() =>
        notifications.error("Failed to update MCP server state"),
      );
  }, []);

  const handleGenerateApiKey = useCallback(() => {
    send("generateApiKey", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to generate API key: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      const newKey = resp.result as string;
      setApiKey(newKey);
      setApiKeyInput(newKey);
      notifications.success("New API key generated and saved");
    });
  }, [send]);

  const confirmClearApiKey = useCallback(() => {
    send("generateApiKey", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to generate API key: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      const newKey = resp.result as string;
      setApiKey(newKey);
      setApiKeyInput(newKey);
      notifications.success("New API key generated and saved");
    });
    setShowApiKeyClearWarning(false);
  }, [send]);

  const handleUpdateSSHKey = useCallback(() => {
    send("setSSHKeyState", { sshKey }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update SSH key: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("SSH key updated successfully");
    });
  }, [send, sshKey]);

  const applyLoopbackOnlyMode = useCallback(
    (enabled: boolean) => {
      send("setLocalLoopbackOnly", { enabled }, resp => {
        if ("error" in resp) {
          notifications.error(
            `Failed to ${enabled ? "enable" : "disable"} loopback-only mode: ${resp.error.data || "Unknown error"}`,
          );
          return;
        }
        setLocalLoopbackOnly(enabled);
        if (enabled) {
          notifications.success(
            "Loopback-only mode enabled. Restart your device to apply.",
          );
        } else {
          notifications.success(
            "Loopback-only mode disabled. Restart your device to apply.",
          );
        }
      });
    },
    [send, setLocalLoopbackOnly],
  );

  const handleLoopbackOnlyModeChange = useCallback(
    (enabled: boolean) => {
      // If trying to enable loopback-only mode, show warning first
      if (enabled) {
        setShowLoopbackWarning(true);
      } else {
        // If disabling, just proceed
        applyLoopbackOnlyMode(false);
      }
    },
    [applyLoopbackOnlyMode, setShowLoopbackWarning],
  );

  const confirmLoopbackModeEnable = useCallback(() => {
    applyLoopbackOnlyMode(true);
    setShowLoopbackWarning(false);
  }, [applyLoopbackOnlyMode, setShowLoopbackWarning]);

  const handleOpenConfigEditor = useCallback(() => {
    send("getConfigRaw", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to load configuration: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setConfigContent(resp.result as string);
      setShowConfigEdit(true);
    });
  }, [send]);

  const handleSaveConfig = useCallback(() => {
    setIsSavingConfig(true);
    send("setConfigRaw", { configStr: configContent }, resp => {
      setIsSavingConfig(false);
      if ("error" in resp) {
        notifications.error(
          `Failed to save configuration: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      notifications.success("Configuration saved successfully");
      setShowConfigEdit(false);
      setShowConfigSavedReboot(true);
    });
  }, [send, configContent]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title={$at("Advanced")}
        description={$at("Access additional settings for troubleshooting and customization")}
      />

      <div className="space-y-4">
        <SettingsItem
          title={$at("Loopback-Only Mode")}
          description={$at("Restrict web interface access to localhost only (127.0.0.1)")}
        noCol
        >
          <Checkbox
            checked={localLoopbackOnly}
            onChange={e => handleLoopbackOnlyModeChange(e.target.checked)}
          />
        </SettingsItem>

        {isOnDevice && (
          <SettingsItem
            title={$at("Developer Mode")}
            description={$at(
              "Expose /debug/pprof and /metrics for troubleshooting. Turn this off when you are done.",
            )}
            noCol
          >
            <Checkbox
              checked={devModeEnabled}
              onChange={e => handleDevModeToggle(e.target.checked)}
            />
          </SettingsItem>
        )}

        {isOnDevice && (
          <div className="space-y-4">
            <SettingsItem
              title={$at("SSH Access")}
              description={$at("Add your SSH public key to enable secure remote access to the device")}
            />
            <div className="space-y-4">
              <TextAreaWithLabel
                label={$at("SSH Public Key")}
                value={sshKey || ""}
                rows={3}
                onChange={e => setSSHKey(e.target.value)}
                placeholder={$at("Enter your SSH public key")}
              />
              <p className="text-xs text-slate-600 dark:text-[#ffffff]">
                {$at("The default SSH user is ")} <strong>root</strong>.
              </p>
              <div className="flex items-center gap-x-2">
                <AntdButton
                  type="primary"
                  onClick={handleUpdateSSHKey}
                  className={isMobile?"w-full":""}
                >
                  {$at("Update SSH Key")}
                </AntdButton>
              </div>
            </div>
          </div>
        )}

        {isOnDevice && (
          <div className="space-y-4">
            <SettingsItem
              title={$at("Enable MCP server")}
              description={$at(
                mcpServerEnabled
                  ? mcpListening
                    ? "MCP server is listening on port 8081"
                    : "Enabled, but not listening on port 8081"
                  : "MCP server is disabled, port 8081 is closed",
              )}
              noCol
            >
              <Checkbox
                checked={mcpServerEnabled}
                onChange={e => handleMcpServerToggle(e.target.checked)}
              />
            </SettingsItem>
            {mcpServerEnabled && (
              <>
                <SettingsItem
                  title={$at("API Key")}
                  description={$at("MCP API key for authenticating MCP clients")}
                />
                <div className="space-y-4">
                  <TextAreaWithLabel
                    label={$at("API Key")}
                    value={apiKeyInput || ""}
                    rows={2}
                    onChange={e => setApiKeyInput(e.target.value)}
                    placeholder={$at("Enter API key or leave empty to auto-generate")}
                  />
                  <p className="text-xs text-slate-600 dark:text-[#ffffff]">
                    {$at(
                      "Used for authenticating MCP requests. The server cannot be enabled without a key.",
                    )}
                  </p>
                  <div className="flex items-center gap-x-2">
                    <AntdButton
                      type="primary"
                      onClick={handleUpdateApiKey}
                      className={isMobile?"w-full":""}
                    >
                      {$at("Save API Key")}
                    </AntdButton>
                    <AntdButton
                      onClick={handleGenerateApiKey}
                      className={isMobile?"w-full":""}
                    >
                      {$at("Generate New")}
                    </AntdButton>
                  </div>
                </div>
              </>
            )}
          </div>
        )}

        <SettingsItem
          title={$at("Force HTTP Transmission")}
          badge="Experimental"
          description={$at("Force using HTTP for video streaming instead of WebRTC")}
          noCol
        >
          <Checkbox
            checked={settings.forceHttp}
            onChange={e => {
              settings.setForceHttp(e.target.checked);
              window.location.reload();
            }}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("USB detection enhancement")}
          description={$at("The DISC state is also checked during USB status retrieval")}
          noCol
        >
          <Checkbox
            checked={usbEnhancedDetectionEnabled}
            onChange={e => handleUsbEnhancedDetectionToggle(e.target.checked)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("USB Emulation")}
          description={$at("Control the USB emulation state")}
        >
          <AntdButton

            type="primary"
            className={`${isMobile?"w-full":""}`}
            onClick={() => handleUsbEmulationToggle(!usbEmulationEnabled)}
          >
            {
              usbEmulationEnabled ? $at("Disable USB Emulation") : $at("Enable USB Emulation")
            }
          </AntdButton>
        </SettingsItem>

        <SettingsItem
          title={$at("USB Gadget Reinitialize")}
          description={$at("Reinitialize USB gadget configuration")}
        >
          <AntdButton
            type="primary"
            className={`${isMobile?"w-full":""}`}
            disabled={isReinitializingGadget}
            loading={isReinitializingGadget}
            onClick={() => {
              if (isReinitializingGadget) return;
              setIsReinitializingGadget(true);
              send("reinitializeUsbGadget", {}, resp => {
                setIsReinitializingGadget(false);
                if ("error" in resp) {
                  notifications.error(
                    `Failed to reinitialize USB gadget: ${resp.error.data || "Unknown error"}`,
                  );
                  return;
                }
                notifications.success("USB gadget reinitialized successfully");
              });
            }}
          >
            {$at("Reinitialize USB Gadget")}
          </AntdButton>
        </SettingsItem>

        <SettingsItem
          title={$at("Reboot System")}
          description={$at("Restart the device system")}
        >
          <AntdButton
            type="primary"
            className={`${isMobile?"w-full":""}`}
            onClick={() => {
              setShowRebootConfirm(true);
            }}
          >
            {$at("Reboot")}
          </AntdButton> 
        </SettingsItem>

        <SettingsItem
          title={$at("Edit Configuration")}
          description={$at("Edit the raw configuration file directly")}
        >
          <AntdButton
            type="primary"
            className={`${isMobile?"w-full":""}`}
            onClick={handleOpenConfigEditor}
          >
            {$at("Edit")}
          </AntdButton>
        </SettingsItem>

        <SettingsItem
          title={$at("Reset Configuration")}
          description={$at("Reset configuration to default. This will log you out.Some configuration changes will take effect after restart system.")}
        >
          <AntdButton
            type="primary"
            className={`${isMobile?"w-full":""}`}
            onClick={() => {
              handleResetConfig();
              window.location.reload();
            }}
          >
            {$at("Reset Config")}
          </AntdButton>
        </SettingsItem>
      </div>

      <ConfirmDialog
        open={showLoopbackWarning}
        onClose={() => {
          setShowLoopbackWarning(false);
        }}
        title="Enable Loopback-Only Mode?"
        description={
          <>
            <p>
              WARNING: This will restrict web interface access to localhost (127.0.0.1)
              only.
            </p>
            <p>Before enabling this feature, make sure you have either:</p>
            <ul className="list-disc space-y-1 pl-5 text-xs text-slate-700 dark:text-slate-300">
              <li>SSH access configured and tested</li>
              <li>Cloud access enabled and working</li>
            </ul>
          </>
        }
        variant="warning"
        confirmText="I Understand, Enable Anyway"
        onConfirm={confirmLoopbackModeEnable}
      />

      <ConfirmDialog
        open={showApiKeyClearWarning}
        onClose={() => {
          setShowApiKeyClearWarning(false);
        }}
        title={$at("Clear API Key?")}
        description={
          <>
            <p>
              {$at("Setting the API key to empty will auto-generate a new random key.")}
            </p>
            <p className="text-xs text-slate-600 dark:text-slate-400 mt-2">
              {$at("Make sure to update your clients with the new key after saving.")}
            </p>
          </>
        }
        variant="warning"
        cancelText={$at("Cancel")}
        confirmText={$at("Generate New Key")}
        onConfirm={confirmClearApiKey}
      />

      <ConfirmDialog
        open={showRebootConfirm}
        onClose={() => {
          setShowRebootConfirm(false);
        }}
        title={$at("Reboot System?")}
        description={
          <>
            <p>
              {$at("Are you sure you want to reboot the system?")}
            </p>
            <p className="text-xs text-slate-600 dark:text-slate-400 mt-2">
              {$at("The device will restart and you will be disconnected from the web interface.")}
            </p>
          </>
        }
        variant="danger"
        cancelText={$at("Cancel")}
        confirmText={$at("Reboot")}
        onConfirm={() => {
          setShowRebootConfirm(false);
          send("reboot", { force: false }, resp => {
            if ("error" in resp) {
              notifications.error(
                `Failed to reboot: ${resp.error.data || "Unknown error"}`,
              );
              return;
            }
            notifications.success("System rebooting...");
          });
        }}
      />

      <ConfirmDialog
        open={showConfigEdit}
        onClose={() => setShowConfigEdit(false)}
        title={$at("Edit Configuration")}
        description={
          <div className="space-y-4">
            <p className="text-xs text-slate-600 dark:text-slate-400">
              {$at("Edit the raw configuration JSON. Be careful when making changes as invalid JSON can cause system issues.")}
            </p>
            <textarea
              value={configContent}
              onChange={e => setConfigContent(e.target.value)}
              className="w-full h-64 p-3 font-mono text-sm bg-gray-50 dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md focus:ring-2 focus:ring-blue-500 focus:border-transparent dark:text-white"
              spellCheck={false}
            />
          </div>
        }
        variant="info"
        cancelText={$at("Cancel")}
        confirmText={isSavingConfig ? `${$at("Saving")}...` : $at("Save")}
        onConfirm={handleSaveConfig}
        isConfirming={isSavingConfig}
      />

      <ConfirmDialog
        open={showConfigSavedReboot}
        onClose={() => setShowConfigSavedReboot(false)}
        title={$at("Configuration Saved")}
        description={
          <>
            <p>
              {$at("Configuration has been saved successfully. Some changes may require a system restart to take effect.")}
            </p>
            <p className="text-xs text-slate-600 dark:text-slate-400 mt-2">
              {$at("Would you like to restart the system now?")}
            </p>
          </>
        }
        variant="info"
        cancelText={$at("Later")}
        confirmText={$at("Restart Now")}
        onConfirm={() => {
          setShowConfigSavedReboot(false);
          send("reboot", { force: false }, resp => {
            if ("error" in resp) {
              notifications.error(
                `Failed to reboot: ${resp.error.data || "Unknown error"}`,
              );
              return;
            }
            notifications.success("System rebooting...");
          });
        }}
      />
    </div>
  );
}
