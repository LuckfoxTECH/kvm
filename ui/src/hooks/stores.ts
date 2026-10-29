import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

import {
  MAX_STEPS_PER_MACRO,
  MAX_TOTAL_MACROS,
  MAX_KEYS_PER_STEP,
} from "@/constants/macros";
import type { KeyboardFormFactor } from "@/keyboardFormFactor";

// Define the JsonRpc types for better type checking
interface JsonRpcResponse {
  jsonrpc: string;
  result?: unknown;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
  id: number | string | null;
}

// Utility function to append stats to a Map
const appendStatToMap = <T extends { timestamp: number }>(
  stat: T,
  prevMap: Map<number, T>,
  maxEntries = 130,
): Map<number, T> => {
  if (prevMap.size > maxEntries) {
    const firstKey = prevMap.keys().next().value;
    if (firstKey !== undefined) {
      prevMap.delete(firstKey);
    }
  }

  const date = Math.floor(stat.timestamp / 1000);
  const newStat = { ...prevMap.get(date), ...stat };
  return new Map(prevMap).set(date, newStat);
};

// Constants and types
export type AvailableSidebarViews = "ConsoleLogViewer" | "MacroMoreList" | "Fullscreen" | "TerminalTabsMobile" | "SettingsModal" | "ClipboardMobile" | "KeyboardPanel" | "MousePanel" | "SettingsVideo"
  | "connection-stats" | "Clipboard" | "PowerControl" | "Macros" | "VirtualMedia" | "SharedFolders" | "UsbEpModeSelect" | "UsbStatusPanel" | "RemoteStatus" | "HdmiStatus" | null;
export type AvailableTerminalTypes = "kvm" | "serial" | "none";

export interface User {
  sub: string;
  email?: string;
  picture?: string;
}

interface UserState {
  user: User | null;
  setUser: (user: User | null) => void;
}

interface UIState {
  sidebarView: AvailableSidebarViews | null;
  setSidebarView: (view: AvailableSidebarViews | null) => void;

  topBarView: AvailableSidebarViews | null;
  setTopBarView: (view: AvailableSidebarViews | null) => void;
  isAnimationComplete: boolean;
  setIsAnimationComplete: (enabled: boolean) => void;

  disableVideoFocusTrap: boolean;
  setDisableVideoFocusTrap: (enabled: boolean) => void;
  isOcrMode: boolean;
  setOcrMode: (enabled: boolean) => void;

  isWakeOnLanModalVisible: boolean;
  setWakeOnLanModalVisibility: (enabled: boolean) => void;

  toggleSidebarView: (view: AvailableSidebarViews) => void;
  toggleTopBarView: (view: AvailableSidebarViews) => void;

  isAttachedVirtualKeyboardVisible: boolean;
  setAttachedVirtualKeyboardVisibility: (enabled: boolean) => void;

  terminalType: AvailableTerminalTypes;
  setTerminalType: (enabled: UIState["terminalType"]) => void;
  otherSession:boolean;
  setOtherSession: (enabled: boolean) => void;

  // Top and bottom bars are hidden by default and act as a single unit.
  barsPinned: boolean;
  setBarsPinned: (pinned: boolean) => void;
  barsTemporarilyVisible: boolean;
  setBarsTemporarilyVisible: (visible: boolean) => void;

  // The floating mobile tool cluster (gesture/mouse/keyboard/reset view).
  mobileToolsHidden: boolean;
  setMobileToolsHidden: (hidden: boolean) => void;
}

export const useUiStore = create<UIState>()(
  persist<UIState, [], [], Pick<UIState, "barsPinned" | "mobileToolsHidden">>(
    set => ({
      terminalType: "none",
      setTerminalType: type => set({ terminalType: type }),

      sidebarView: null,
      setSidebarView: view => set({ sidebarView: view }),
      topBarView: null,
      setTopBarView: view => set({ topBarView: view }),

      disableVideoFocusTrap: false,
      setDisableVideoFocusTrap: enabled => set({ disableVideoFocusTrap: enabled }),
      isOcrMode: false,
      setOcrMode: enabled => set({ isOcrMode: enabled }),
      isAnimationComplete: false,
      setIsAnimationComplete: enabled => set({ isAnimationComplete: enabled }),

      isWakeOnLanModalVisible: false,
      setWakeOnLanModalVisibility: enabled => set({ isWakeOnLanModalVisible: enabled }),

      toggleSidebarView: view =>
        set(state => {
          if (state.sidebarView === view) {
            return { sidebarView: null, topBarView: null };
          } else {
            return { sidebarView: view, topBarView: null };
          }
        }),
      toggleTopBarView: view =>
        set(state => {
          if (state.topBarView === view) {
            return { topBarView: null, sidebarView: null };
          } else {
            return { topBarView: view, sidebarView: null };
          }
        }),
      otherSession: false,
      setOtherSession: enabled => set({ otherSession: enabled }),

      isAttachedVirtualKeyboardVisible: true,
      setAttachedVirtualKeyboardVisibility: enabled =>
        set({ isAttachedVirtualKeyboardVisible: enabled }),

      barsPinned: false,
      setBarsPinned: pinned => set({ barsPinned: pinned }),
      barsTemporarilyVisible: false,
      setBarsTemporarilyVisible: visible => set({ barsTemporarilyVisible: visible }),

      mobileToolsHidden: false,
      setMobileToolsHidden: hidden => set({ mobileToolsHidden: hidden }),
    }),
    {
      name: "ui-bars",
      storage: createJSONStorage(() => localStorage),
      // Only explicit choices survive a reload; the transient reveal does not.
      partialize: state => ({ barsPinned: state.barsPinned, mobileToolsHidden: state.mobileToolsHidden }),
    },
  ),
);

interface RTCState {
  peerConnection: RTCPeerConnection | null;
  setPeerConnection: (pc: RTCState["peerConnection"]) => void;

  setRpcDataChannel: (channel: RTCDataChannel) => void;
  rpcDataChannel: RTCDataChannel | null;

  hidRpcDisabled: boolean;
  setHidRpcDisabled: (disabled: boolean) => void;

  rpcHidProtocolVersion: number | null;
  setRpcHidProtocolVersion: (version: number | null) => void;

  rpcHidChannel: RTCDataChannel | null;
  setRpcHidChannel: (channel: RTCDataChannel | null) => void;

  rpcHidUnreliableChannel: RTCDataChannel | null;
  setRpcHidUnreliableChannel: (channel: RTCDataChannel | null) => void;

  rpcHidUnreliableNonOrderedChannel: RTCDataChannel | null;
  setRpcHidUnreliableNonOrderedChannel: (channel: RTCDataChannel | null) => void;

  diskChannel: RTCDataChannel | null;
  setDiskChannel: (channel: RTCDataChannel) => void;

  peerConnectionState: RTCPeerConnectionState | null;
  setPeerConnectionState: (state: RTCPeerConnectionState) => void;

  transceiver: RTCRtpTransceiver | null;
  setTransceiver: (transceiver: RTCRtpTransceiver) => void;

  audioTransceiver: RTCRtpTransceiver | null;
  setAudioTransceiver: (transceiver: RTCRtpTransceiver) => void;

  mediaStream: MediaStream | null;
  setMediaStream: (stream: MediaStream) => void;

  videoStreamStats: RTCInboundRtpStreamStats | null;
  appendVideoStreamStats: (state: RTCInboundRtpStreamStats) => void;
  videoStreamStatsHistory: Map<number, RTCInboundRtpStreamStats>;

  audioStreamStats: RTCInboundRtpStreamStats | null;
  appendAudioStreamStats: (state: RTCInboundRtpStreamStats) => void;
  audioStreamStatsHistory: Map<number, RTCInboundRtpStreamStats>;

  isTurnServerInUse: boolean;
  setTurnServerInUse: (inUse: boolean) => void;

  inboundRtpStats: Map<number, RTCInboundRtpStreamStats>;
  appendInboundRtpStats: (state: RTCInboundRtpStreamStats) => void;
  clearInboundRtpStats: () => void;

  candidatePairStats: Map<number, RTCIceCandidatePairStats>;
  appendCandidatePairStats: (pair: RTCIceCandidatePairStats) => void;
  clearCandidatePairStats: () => void;

  // Remote ICE candidates stat type doesn't exist as of today
  localCandidateStats: Map<number, RTCIceCandidateStats>;
  appendLocalCandidateStats: (stats: RTCIceCandidateStats) => void;

  remoteCandidateStats: Map<number, RTCIceCandidateStats>;
  appendRemoteCandidateStats: (stats: RTCIceCandidateStats) => void;

  // Disk data channel stats type doesn't exist as of today
  diskDataChannelStats: Map<number, RTCDataChannelStats>;
  appendDiskDataChannelStats: (stat: RTCDataChannelStats) => void;

  terminalChannel: RTCDataChannel | null;
  setTerminalChannel: (channel: RTCDataChannel | null) => void;

  kvmTerminal: RTCDataChannel | null;
  setKvmTerminal: (channel: RTCDataChannel | null) => void;

  serialConsole: RTCDataChannel | null;
  setSerialConsole: (channel: RTCDataChannel | null) => void;
}

export const useRTCStore = create<RTCState>(set => ({
  peerConnection: null,
  setPeerConnection: pc => set({ peerConnection: pc }),

  rpcDataChannel: null,
  setRpcDataChannel: channel => set({ rpcDataChannel: channel }),

  hidRpcDisabled: false,
  setHidRpcDisabled: disabled => set({ hidRpcDisabled: disabled }),

  rpcHidProtocolVersion: null,
  setRpcHidProtocolVersion: version => set({ rpcHidProtocolVersion: version }),

  rpcHidChannel: null,
  setRpcHidChannel: channel => set({ rpcHidChannel: channel }),

  rpcHidUnreliableChannel: null,
  setRpcHidUnreliableChannel: channel => set({ rpcHidUnreliableChannel: channel }),

  rpcHidUnreliableNonOrderedChannel: null,
  setRpcHidUnreliableNonOrderedChannel: channel =>
    set({ rpcHidUnreliableNonOrderedChannel: channel }),

  transceiver: null,
  setTransceiver: transceiver => set({ transceiver }),

  audioTransceiver: null,
  setAudioTransceiver: audioTransceiver => set({ audioTransceiver }),
  
  peerConnectionState: null,
  setPeerConnectionState: state => set({ peerConnectionState: state }),

  diskChannel: null,
  setDiskChannel: channel => set({ diskChannel: channel }),

  mediaStream: null,
  setMediaStream: stream => set({ mediaStream: stream }),

  videoStreamStats: null,
  appendVideoStreamStats: stats => set({ videoStreamStats: stats }),
  videoStreamStatsHistory: new Map(),

  audioStreamStats: null,
  appendAudioStreamStats: stats => set({ audioStreamStats: stats }),
  audioStreamStatsHistory: new Map(),
  
  isTurnServerInUse: false,
  setTurnServerInUse: inUse => set({ isTurnServerInUse: inUse }),

  inboundRtpStats: new Map(),
  appendInboundRtpStats: newStat => {
    set(prevState => ({
      inboundRtpStats: appendStatToMap(newStat, prevState.inboundRtpStats),
    }));
  },
  clearInboundRtpStats: () => set({ inboundRtpStats: new Map() }),

  candidatePairStats: new Map(),
  appendCandidatePairStats: newStat => {
    set(prevState => ({
      candidatePairStats: appendStatToMap(newStat, prevState.candidatePairStats),
    }));
  },
  clearCandidatePairStats: () => set({ candidatePairStats: new Map() }),

  localCandidateStats: new Map(),
  appendLocalCandidateStats: newStat => {
    set(prevState => ({
      localCandidateStats: appendStatToMap(newStat, prevState.localCandidateStats),
    }));
  },

  remoteCandidateStats: new Map(),
  appendRemoteCandidateStats: newStat => {
    set(prevState => ({
      remoteCandidateStats: appendStatToMap(newStat, prevState.remoteCandidateStats),
    }));
  },

  diskDataChannelStats: new Map(),
  appendDiskDataChannelStats: newStat => {
    set(prevState => ({
      diskDataChannelStats: appendStatToMap(newStat, prevState.diskDataChannelStats),
    }));
  },

  // Add these new properties to the store implementation
  terminalChannel: null,
  setTerminalChannel: channel => set({ terminalChannel: channel }),
  kvmTerminal: null,
  setKvmTerminal: channel => set({ kvmTerminal: channel }),

 serialConsole: null,
  setSerialConsole: channel => set({ serialConsole: channel }),
}));

interface MouseMove {
  x: number;
  y: number;
  buttons: number;
}
interface MouseState {
  mouseX: number;
  mouseY: number;
  mouseMove?: MouseMove;
  setMouseMove: (move?: MouseMove) => void;
  setMousePosition: (x: number, y: number) => void;
}

export const useMouseStore = create<MouseState>(set => ({
  mouseX: 0,
  mouseY: 0,
  setMouseMove: (move?: MouseMove) => set({ mouseMove: move }),
  setMousePosition: (x, y) => set({ mouseX: x, mouseY: y }),
}));

export interface VideoState {
  width: number;
  height: number;
  clientWidth: number;
  clientHeight: number;
  setClientSize: (width: number, height: number) => void;
  setSize: (width: number, height: number) => void;
  hdmiState: "ready" | "no_signal" | "no_lock" | "out_of_range" | "connecting";
  setHdmiState: (state: {
    ready: boolean;
    error?: Extract<VideoState["hdmiState"], "no_signal" | "no_lock" | "out_of_range">;
  }) => void;
  // Raw values last reported by the device's video input event. Unlike
  // width/height (decoded stream size) these are the HDMI source timings, so
  // they stay 0 while there is no signal.
  inputWidth: number;
  inputHeight: number;
  inputFps: number;
  setVideoInput: (state: { width: number; height: number; fps: number }) => void;
}

export interface BacklightSettings {
  /** Brightness percentage (0-100), same unit as the touchscreen slider. */
  max_brightness: number;
  dim_after: number;
  off_after: number;
}

export const useVideoStore = create<VideoState>(set => ({
  width: 0,
  height: 0,

  clientWidth: 0,
  clientHeight: 0,

  // The video element's client size
  setClientSize: (clientWidth, clientHeight) => set({ clientWidth, clientHeight }),

  // Resolution
  setSize: (width, height) => set({ width, height }),

  hdmiState: "connecting",
  inputWidth: 0,
  inputHeight: 0,
  inputFps: 0,
  setVideoInput: ({ width, height, fps }) =>
    set({ inputWidth: width, inputHeight: height, inputFps: fps }),
  setHdmiState: state => {
    if (!state) return;
    const { ready, error } = state;

    if (ready) {
      return set({ hdmiState: "ready" });
    } else if (error) {
      return set({ hdmiState: error });
    } else {
      return set({ hdmiState: "connecting" });
    }
  },
}));

// There is no "auto": the client picks a source, because whether the controlled
// machine reports its lock lights at all is a property of that machine, not
// something the UI can guess from here.
export type KeyboardLedSync = "browser" | "host";

interface SettingsState {
  language: string;
  setLanguage: (language: string) => void;

  isCursorHidden: boolean;
  setCursorVisibility: (enabled: boolean) => void;

  mouseMode: string;
  setMouseMode: (mode: string) => void;

  debugMode: boolean;
  setDebugMode: (enabled: boolean) => void;

  // Add new developer mode state
  developerMode: boolean;
  setDeveloperMode: (enabled: boolean) => void;

  displayRotation: string;
  setDisplayRotation: (rotation: string) => void;

  backlightSettings: BacklightSettings;
  setBacklightSettings: (settings: BacklightSettings) => void;

  /** Whether the panel is currently lit, kept in sync by the `displayState` event. */
  displayOn: boolean;
  setDisplayOn: (on: boolean) => void;
  
  timeZone: string;
  setTimeZone: (timezone: string) => void;

  ledGreenMode: string;
  setLedGreenMode: (mode: string) => void;

  ledYellowMode: string;
  setLedYellowMode: (mode: string) => void;

  keyboardLayout: string;
  setKeyboardLayout: (layout: string) => void;

  keyboardFormFactor: KeyboardFormFactor;
  setKeyboardFormFactor: (formFactor: KeyboardFormFactor) => void;

  actionBarCtrlAltDel: boolean;
  setActionBarCtrlAltDel: (enabled: boolean) => void;

  keyboardLedSync: KeyboardLedSync;
  setKeyboardLedSync: (sync: KeyboardLedSync) => void;

  scrollThrottling: number;
  setScrollThrottling: (value: number) => void;

  mouseSensitivity: number;
  setMouseSensitivity: (value: number) => void;

  invertScroll: boolean;
  setInvertScroll: (enabled: boolean) => void;

  showPressedKeys: boolean;
  setShowPressedKeys: (show: boolean) => void;

  // Swaps the Command (GUI) and Control modifier bits on the way out, so a key in
  // the Control position sends Command to the host - what macOS users expect.
  swapCommandControl: boolean;
  setSwapCommandControl: (enabled: boolean) => void;

  overrideCtrlV: boolean;
  setOverrideCtrlV: (enabled: boolean) => void;
  pasteShortcutEnabled: boolean;
  setPasteShortcutEnabled: (enabled: boolean) => void;
  pasteShortcut: string;
  setPasteShortcut: (shortcut: string) => void;
  ocrShortcutEnabled: boolean;
  setOcrShortcutEnabled: (enabled: boolean) => void;
  ocrShortcut: string;
  setOcrShortcut: (shortcut: string) => void;

  // Video enhancement settings
  videoSaturation: number;
  setVideoSaturation: (value: number) => void;
  videoBrightness: number;
  setVideoBrightness: (value: number) => void;
  videoContrast: number;
  setVideoContrast: (value: number) => void;

  forceHttp: boolean;
  setForceHttp: (enabled: boolean) => void;
}

export const useSettingsStore = create(
  persist<SettingsState>(
    set => ({
      language: "en",
      setLanguage: language => set({ language }),

      isCursorHidden: false,
      setCursorVisibility: enabled => set({ isCursorHidden: enabled }),

      mouseMode: "absolute",
      setMouseMode: mode => set({ mouseMode: mode }),

      debugMode: import.meta.env.DEV,
      setDebugMode: enabled => set({ debugMode: enabled }),

      // Add developer mode with default value
      developerMode: false,
      setDeveloperMode: enabled => set({ developerMode: enabled }),

      displayRotation: "180",
      setDisplayRotation: (rotation: string) => set({ displayRotation: rotation }),

      backlightSettings: {
        max_brightness: 32,
        dim_after: 10000,
        off_after: 50000,
      },
      setBacklightSettings: (settings: BacklightSettings) =>
        set({ backlightSettings: settings }),

      displayOn: true,
      setDisplayOn: (on: boolean) => set({ displayOn: on }),

      timeZone: "CST-8",
      setTimeZone: (timezone: string) => set({ timeZone: timezone }),

      ledGreenMode: "network-rx",
      setLedGreenMode: (mode: string) => set({ ledGreenMode: mode }),

      ledYellowMode: "activity",
      setLedYellowMode: (mode: string) => set({ ledYellowMode: mode }),

      keyboardLayout: "en-US",
      setKeyboardLayout: layout => set({ keyboardLayout: layout }),

      keyboardFormFactor: "ANSI",
      setKeyboardFormFactor: formFactor => set({ keyboardFormFactor: formFactor }),

      actionBarCtrlAltDel: false,
      setActionBarCtrlAltDel: enabled => set({ actionBarCtrlAltDel: enabled }),

      keyboardLedSync: "host",
      setKeyboardLedSync: sync => set({ keyboardLedSync: sync }),

      scrollThrottling: 0,
      setScrollThrottling: value => set({ scrollThrottling: value }),

      mouseSensitivity: 1.0,
      setMouseSensitivity: value => set({ mouseSensitivity: value }),

      invertScroll: false,
      setInvertScroll: enabled => set({ invertScroll: enabled }),

      showPressedKeys: true,
      setShowPressedKeys: show => set({ showPressedKeys: show }),

      swapCommandControl: false,
      setSwapCommandControl: enabled => set({ swapCommandControl: enabled }),

      overrideCtrlV: false,
      setOverrideCtrlV: enabled => set({ overrideCtrlV: enabled }),
      pasteShortcutEnabled: true,
      setPasteShortcutEnabled: enabled => set({ pasteShortcutEnabled: enabled }),
      pasteShortcut: "Ctrl+V",
      setPasteShortcut: shortcut => set({ pasteShortcut: shortcut }),
      ocrShortcutEnabled: true,
      setOcrShortcutEnabled: enabled => set({ ocrShortcutEnabled: enabled }),
      ocrShortcut: "Ctrl+C",
      setOcrShortcut: shortcut => set({ ocrShortcut: shortcut }),

      // Video enhancement settings with default values (1.0 = normal)
      videoSaturation: 1.0,
      setVideoSaturation: value => set({ videoSaturation: value }),
      videoBrightness: 1.0,
      setVideoBrightness: value => set({ videoBrightness: value }),
      videoContrast: 1.0,
      setVideoContrast: value => set({ videoContrast: value }),

      forceHttp: false,
      setForceHttp: enabled => set({ forceHttp: enabled }),
    }),
    {
      name: "settings",
      storage: createJSONStorage(() => localStorage),
      // "auto" was a stored default that could never be picked in the UI. Now that
      // the choice is explicit, profiles still carrying it land on host-only.
      merge: (persisted, current) => {
        const merged = { ...current, ...(persisted as Partial<SettingsState>) };
        merged.keyboardLedSync = merged.keyboardLedSync === "browser" ? "browser" : "host";
        return merged;
      },
    },
  ),
);

export interface RemoteVirtualMediaState {
  source: "WebRTC" | "HTTP" | "Storage" | "SDStorage" | null;
  mode: "CDROM" | "Disk" | null;
  filename: string | null;
  url: string | null;
  path: string | null;
  size: number | null;
}

export interface MountMediaState {
  localFile: File | null;
  setLocalFile: (file: MountMediaState["localFile"]) => void;

  remoteVirtualMediaState: RemoteVirtualMediaState | null;
  setRemoteVirtualMediaState: (state: MountMediaState["remoteVirtualMediaState"]) => void;

  modalView: "mode" | "browser" | "url" | "device" | "sd" | "upload" | "upload_sd" | "error" | "mtp_device" | "mtp_sd" |  null;
  setModalView: (view: MountMediaState["modalView"]) => void;

  isMountMediaDialogOpen: boolean;
  setIsMountMediaDialogOpen: (isOpen: MountMediaState["isMountMediaDialogOpen"]) => void;

  uploadedFiles: { name: string; size: string; uploadedAt: string }[];
  addUploadedFile: (file: { name: string; size: string; uploadedAt: string }) => void;

  errorMessage: string | null;
  setErrorMessage: (message: string | null) => void;
}

export const useMountMediaStore = create<MountMediaState>(set => ({
  localFile: null,
  setLocalFile: file => set({ localFile: file }),

  remoteVirtualMediaState: null,
  setRemoteVirtualMediaState: state => set({ remoteVirtualMediaState: state }),

  modalView: "mode",
  setModalView: view => set({ modalView: view }),

  isMountMediaDialogOpen: false,
  setIsMountMediaDialogOpen: isOpen => set({ isMountMediaDialogOpen: isOpen }),

  uploadedFiles: [],
  addUploadedFile: file =>
    set(state => ({ uploadedFiles: [...state.uploadedFiles, file] })),

  errorMessage: null,
  setErrorMessage: message => set({ errorMessage: message }),
}));

export const hidKeyBufferSize = 6;
export const hidErrorRollOver = 0x01;

export interface KeysDownState {
  modifier: number;
  keys: number[];
}

export interface KeyboardLedState {
  num_lock: boolean;
  caps_lock: boolean;
  scroll_lock: boolean;
  compose: boolean;
  kana: boolean;
  // shift is not part of the main USB HID spec; the device reports it when the
  // host lights bit 6. Older firmware never sends it.
  shift?: boolean;
  // reported tells "lights off" apart from "nobody has told us yet": the lamps
  // render a grey question mark until something claims to know. Both the device
  // LED report and the browser's own getModifierState set it, because either is
  // a real answer for the mode in force. Switching the LED source therefore has
  // to clear it (resetKeyboardLedTruth), otherwise a browser answer would
  // survive into host mode and be drawn as if the host had reported it.
  reported?: boolean;
};
const defaultKeyboardLedState: KeyboardLedState = {
  num_lock: false,
  caps_lock: false,
  scroll_lock: false,
  compose: false,
  kana: false,
};

export interface HidState {
  activeKeys: number[];
  activeModifiers: number[];

  updateActiveKeysAndModifiers: (keysAndModifiers: {
    keys: number[];
    modifiers: number[];
  }) => void;

  altGrArmed: boolean;
  setAltGrArmed: (armed: boolean) => void;

  altGrTimer: number | null; // _altGrCtrlTime
  setAltGrTimer: (timeout: number | null) => void;

  altGrCtrlTime: number; // _altGrCtrlTime
  setAltGrCtrlTime: (time: number) => void;

  keyboardLedState?: KeyboardLedState;
  setKeyboardLedState: (state: KeyboardLedState) => void;
  setIsNumLockActive: (active: boolean) => void;
  setIsCapsLockActive: (active: boolean) => void;
  setIsScrollLockActive: (active: boolean) => void;

  keyboardLedStateSyncAvailable: boolean;
  setKeyboardLedStateSyncAvailable: (available: boolean) => void;
  // Drops whatever the previous LED source told us, so the lamps go back to
  // unknown instead of showing a value that belongs to a mode we left.
  resetKeyboardLedTruth: () => void;

  keysDownState?: { modifier: number; keys: number[] };
  setKeysDownState: (state: { modifier: number; keys: number[] }) => void;

  isVirtualKeyboardEnabled: boolean;
  setVirtualKeyboardEnabled: (enabled: boolean) => void;
  allowTapToOpenVirtualKeyboard: boolean;
  setAllowTapToOpenVirtualKeyboard: (enabled: boolean) => void;

  isPasteModeEnabled: boolean;
  setPasteModeEnabled: (enabled: boolean) => void;

  usbState: "configured" | "attached" | "not attached" | "suspended" | "addressed" | "default";
  setUsbState: (state: HidState["usbState"]) => void;
  
  isReinitializingGadget: boolean;
  setIsReinitializingGadget: (reinitializing: boolean) => void;

  // Mirrors of the device side USB gadget functions. Input handlers read these
  // to stop capturing keyboard/mouse traffic while the corresponding HID
  // interface is not attached, which would otherwise keep failing or retrigger
  // a gadget reinitialization.
  usbEmulationEnabled: boolean;
  setUsbEmulationEnabled: (enabled: boolean) => void;
  usbKeyboardEnabled: boolean;
  setUsbKeyboardEnabled: (enabled: boolean) => void;
  usbMouseEnabled: boolean;
  setUsbMouseEnabled: (enabled: boolean) => void;
}

export interface SerialState {
  isConnected: boolean;
  setIsConnected: (connected: boolean) => void;
}

export const useSerialStore = create<SerialState>(set => ({
  isConnected: false,
  setIsConnected: connected => set({ isConnected: connected }),
}));

export const useHidStore = create<HidState>((set, get) => ({
  activeKeys: [],
  activeModifiers: [],
  updateActiveKeysAndModifiers: ({ keys, modifiers }) => {
    return set({ activeKeys: keys, activeModifiers: modifiers });
  },

  altGrArmed: false,
  setAltGrArmed: armed => set({ altGrArmed: armed }),

  altGrTimer: 0,
  setAltGrTimer: timeout => set({ altGrTimer: timeout }),

  altGrCtrlTime: 0,
  setAltGrCtrlTime: time => set({ altGrCtrlTime: time }),

  setKeyboardLedState: ledState => {
    // Device pushes and query responses cannot supply browser-mode truth.
    if (useSettingsStore.getState().keyboardLedSync !== "host") return;
    set({ keyboardLedState: ledState });
  },
  setIsNumLockActive: active => {
    if (useSettingsStore.getState().keyboardLedSync !== "browser") return;
    const keyboardLedState = { ...(get().keyboardLedState || defaultKeyboardLedState) };
    keyboardLedState.num_lock = active;
    // The browser now holds local truth for the lamps, so stop rendering them as
    // unknown. This is the browser-managed path only; host mode never gets here.
    keyboardLedState.reported = true;
    set({ keyboardLedState });
  },
  setIsCapsLockActive: active => {
    if (useSettingsStore.getState().keyboardLedSync !== "browser") return;
    const keyboardLedState = { ...(get().keyboardLedState || defaultKeyboardLedState) };
    keyboardLedState.caps_lock = active;
    keyboardLedState.reported = true;
    set({ keyboardLedState });
  },
  setIsScrollLockActive: active => {
    if (useSettingsStore.getState().keyboardLedSync !== "browser") return;
    const keyboardLedState = { ...(get().keyboardLedState || defaultKeyboardLedState) };
    keyboardLedState.scroll_lock = active;
    keyboardLedState.reported = true;
    set({ keyboardLedState });
  },

  keysDownState: undefined,
  setKeysDownState: state => set({ keysDownState: state }),

  keyboardLedStateSyncAvailable: false,
  setKeyboardLedStateSyncAvailable: available => set({ keyboardLedStateSyncAvailable: available }),
  resetKeyboardLedTruth: () =>
    set({
      keyboardLedState: { ...defaultKeyboardLedState },
      keyboardLedStateSyncAvailable: false,
    }),

  isVirtualKeyboardEnabled: false,
  setVirtualKeyboardEnabled: enabled => set({ isVirtualKeyboardEnabled: enabled }),
  allowTapToOpenVirtualKeyboard: true,
  setAllowTapToOpenVirtualKeyboard: enabled => set({ allowTapToOpenVirtualKeyboard: enabled }),

  isPasteModeEnabled: false,
  setPasteModeEnabled: enabled => set({ isPasteModeEnabled: enabled }),

  // Add these new properties for USB state
  usbState: "not attached",
  setUsbState: state => set({ usbState: state }),
  
  isReinitializingGadget: false,
  setIsReinitializingGadget: reinitializing => set({ isReinitializingGadget: reinitializing }),

  // Default to enabled: until the device reports otherwise, input has to keep
  // working exactly as before.
  usbEmulationEnabled: true,
  setUsbEmulationEnabled: enabled => set({ usbEmulationEnabled: enabled }),
  usbKeyboardEnabled: true,
  setUsbKeyboardEnabled: enabled => set({ usbKeyboardEnabled: enabled }),
  usbMouseEnabled: true,
  setUsbMouseEnabled: enabled => set({ usbMouseEnabled: enabled }),
}));


export interface UsbEpModeStore {
  usbEpMode: string;
  setUsbEpMode: (mode: string) => void;
}

export const useUsbEpModeStore = create<UsbEpModeStore>(set => ({
  usbEpMode: "disabled",
  setUsbEpMode: (mode: string) => set({ usbEpMode: mode }),
}));

export interface AudioModeStore {
  audioMode: string;
  setAudioMode: (mode: string) => void;
}
export const useAudioModeStore = create<AudioModeStore>(set => ({
  audioMode: "disabled",
  setAudioMode: (mode: string) => set({ audioMode: mode }),
}));

export const useUserStore = create<UserState>(set => ({
  user: null,
  setUser: user => set({ user }),
}));

export interface UpdateState {
  isUpdatePending: boolean;
  setIsUpdatePending: (isPending: boolean) => void;
  updateDialogHasBeenMinimized: boolean;
  versionUpdateSource: string;
  setVersionUpdateSource: (source: string) => void;
  versionLocalPackageInfo: {
    appVersion: string;
    systemVersion: string;
    hasApp: boolean;
    hasSystem: boolean;
  } | null;
  setVersionLocalPackageInfo: (
    info: {
      appVersion: string;
      systemVersion: string;
      hasApp: boolean;
      hasSystem: boolean;
    } | null,
  ) => void;
  otaState: {
    updating: boolean;
    error: string | null;

    metadataFetchedAt: string | null;

    // App update
    appUpdatePending: boolean;

    appDownloadProgress: number;
    appDownloadSpeedBps: number;
    appDownloadFinishedAt: string | null;

    appVerificationProgress: number;
    appVerifiedAt: string | null;

    appUpdateProgress: number;
    appUpdatedAt: string | null;

    // System update
    systemUpdatePending: boolean;

    systemDownloadProgress: number;
    systemDownloadSpeedBps: number;
    systemDownloadFinishedAt: string | null;

    systemVerificationProgress: number;
    systemVerifiedAt: string | null;

    systemUpdateProgress: number;
    systemUpdatedAt: string | null;

    appSignatureMissing: boolean;
    systemSignatureMissing: boolean;
    appSignatureAbsent: boolean;
    appSignatureInvalid: boolean;
    appNoPublicKey: boolean;
    signatureVerified: boolean;
  };
  setOtaState: (state: UpdateState["otaState"]) => void;
  setUpdateDialogHasBeenMinimized: (hasBeenMinimized: boolean) => void;
  modalView:
    | "loading"
    | "updating"
    | "upToDate"
    | "updateAvailable"
    | "updateCompleted"
    | "error";
  setModalView: (view: UpdateState["modalView"]) => void;
  setUpdateErrorMessage: (errorMessage: string) => void;
  updateErrorMessage: string | null;
}

export const useUpdateStore = create<UpdateState>(set => ({
  isUpdatePending: false,
  setIsUpdatePending: isPending => set({ isUpdatePending: isPending }),
  versionUpdateSource: "github",
  setVersionUpdateSource: source => set({ versionUpdateSource: source }),
  versionLocalPackageInfo: null,
  setVersionLocalPackageInfo: info => set({ versionLocalPackageInfo: info }),

  setOtaState: state =>
    set(current => {
      const definedEntries = Object.entries(state).filter(([, value]) => value !== undefined);
      const merged = {
        ...current.otaState,
        ...Object.fromEntries(definedEntries),
      } as UpdateState["otaState"];
      return { otaState: merged };
    }),
  otaState: {
    updating: false,
    error: null,
    metadataFetchedAt: null,
    appUpdatePending: false,
    systemUpdatePending: false,
    appDownloadProgress: 0,
    appDownloadSpeedBps: 0,
    appDownloadFinishedAt: null,
    appVerificationProgress: 0,
    appVerifiedAt: null,
    systemDownloadProgress: 0,
    systemDownloadSpeedBps: 0,
    systemDownloadFinishedAt: null,
    systemVerificationProgress: 0,
    systemVerifiedAt: null,
    appUpdateProgress: 0,
    appUpdatedAt: null,
    systemUpdateProgress: 0,
    systemUpdatedAt: null,
    appSignatureMissing: false,
    systemSignatureMissing: false,
    appSignatureAbsent: false,
    appSignatureInvalid: false,
    appNoPublicKey: false,
    signatureVerified: false,
  },

  updateDialogHasBeenMinimized: false,
  setUpdateDialogHasBeenMinimized: hasBeenMinimized =>
    set({ updateDialogHasBeenMinimized: hasBeenMinimized }),
  modalView: "loading",
  setModalView: view => set({ modalView: view }),
  updateErrorMessage: null,
  setUpdateErrorMessage: errorMessage => set({ updateErrorMessage: errorMessage }),
}));

interface UsbConfigModalState {
  modalView: "updateUsbConfig" | "updateUsbConfigSuccess";
  errorMessage: string | null;
  setModalView: (view: UsbConfigModalState["modalView"]) => void;
  setErrorMessage: (message: string | null) => void;
}

export interface UsbConfigState {
  vendor_id: string;
  product_id: string;
  serial_number: string;
  manufacturer: string;
  product: string;
}

export const useUsbConfigModalStore = create<UsbConfigModalState>(set => ({
  modalView: "updateUsbConfig",
  errorMessage: null,
  setModalView: view => set({ modalView: view }),
  setErrorMessage: message => set({ errorMessage: message }),
}));

export interface LocalAuthModalState {
  modalView:
    | "createPassword"
    | "deletePassword"
    | "updatePassword"
    | "creationSuccess"
    | "deleteSuccess"
    | "updateSuccess";
  setModalView: (view: LocalAuthModalState["modalView"]) => void;
}

export const useLocalAuthModalStore = create<LocalAuthModalState>(set => ({
  modalView: "createPassword",
  setModalView: view => set({ modalView: view }),
}));

export interface DeviceState {
  appVersion: string | null;
  systemVersion: string | null;

  setAppVersion: (version: string) => void;
  setSystemVersion: (version: string) => void;
}

export const useDeviceStore = create<DeviceState>(set => ({
  appVersion: null,
  systemVersion: null,

  setAppVersion: version => set({ appVersion: version }),
  setSystemVersion: version => set({ systemVersion: version }),
}));

export interface DhcpLease {
  ip?: string;
  netmask?: string;
  broadcast?: string;
  ttl?: string;
  mtu?: string;
  hostname?: string;
  domain?: string;
  bootp_next_server?: string;
  bootp_server_name?: string;
  bootp_file?: string;
  timezone?: string;
  routers?: string[];
  dns?: string[];
  ntp_servers?: string[];
  lpr_servers?: string[];
  _time_servers?: string[];
  _name_servers?: string[];
  _log_servers?: string[];
  _cookie_servers?: string[];
  _wins_servers?: string[];
  _swap_server?: string;
  boot_size?: string;
  root_path?: string;
  lease?: string;
  lease_expiry?: Date;
  dhcp_type?: string;
  server_id?: string;
  message?: string;
  tftp?: string;
  bootfile?: string;
}

export interface IPv6Address {
  address: string;
  prefix: string;
  valid_lifetime: string;
  preferred_lifetime: string;
  scope: string;
}

export interface NetworkState {
  interface_name?: string;
  mac_address?: string;
  ipv4?: string;
  ipv4_addresses?: string[];
  ipv6?: string;
  ipv6_addresses?: IPv6Address[];
  ipv6_link_local?: string;
  dhcp_lease?: DhcpLease;

  setNetworkState: (state: NetworkState) => void;
  setDhcpLease: (lease: NetworkState["dhcp_lease"]) => void;
  setDhcpLeaseExpiry: (expiry: Date) => void;
}

export type IPv6Mode =
  | "disabled"
  | "slaac"
  | "dhcpv6"
  | "slaac_and_dhcpv6"
  | "static"
  | "link_local"
  | "unknown";
export type IPv4Mode = "disabled" | "static" | "dhcp" | "unknown";
export type LLDPMode = "disabled" | "basic" | "all" | "unknown";
export type mDNSMode = "disabled" | "auto" | "ipv4_only" | "ipv6_only" | "unknown";
export type TimeSyncMode =
  | "ntp_only"
  | "ntp_and_http"
  | "http_only"
  | "custom"
  | "unknown";

export interface IPv4StaticConfig {
  address?: string;
  netmask?: string;
  gateway?: string;
  dns?: string[];
}

export interface TimeSyncCustomServer {
  server: string;
  enabled: boolean;
}

export interface NetworkSettings {
  hostname: string;
  domain: string;
  http_proxy?: string;
  https_proxy?: string;
  all_proxy?: string;
  ipv4_mode: IPv4Mode;
  ipv4_request_address?: string;
  ipv4_static?: IPv4StaticConfig;
  ipv6_mode: IPv6Mode;
  lldp_mode: LLDPMode;
  lldp_tx_tlvs: string[];
  mdns_mode: mDNSMode;
  time_sync_mode: TimeSyncMode;
  time_sync_ntp_servers?: string[];
  time_sync_http_urls?: string[];
  time_sync_ntp_custom?: TimeSyncCustomServer[];
  time_sync_http_custom?: TimeSyncCustomServer[];
  pending_reboot?: boolean;
}

export const useNetworkStateStore = create<NetworkState>((set, get) => ({
  setNetworkState: (state: NetworkState) => set(state),
  setDhcpLease: (lease: NetworkState["dhcp_lease"]) => set({ dhcp_lease: lease }),
  setDhcpLeaseExpiry: (expiry: Date) => {
    const lease = get().dhcp_lease;
    if (!lease) {
      console.warn("No lease found");
      return;
    }

    lease.lease_expiry = expiry;
    set({ dhcp_lease: lease });
  },
}));

export interface KeySequenceStep {
  keys: string[];
  modifiers: string[];
  delay: number;
}

export interface KeySequence {
  id: string;
  name: string;
  steps: KeySequenceStep[];
  sortOrder?: number;
}

export interface MacrosState {
  macros: KeySequence[];
  loading: boolean;
  initialized: boolean;
  loadMacros: () => Promise<void>;
  saveMacros: (macros: KeySequence[]) => Promise<void>;
  sendFn:
    | ((
        method: string,
        params: unknown,
        callback?: ((resp: JsonRpcResponse) => void) | undefined,
      ) => void)
    | null;
  setSendFn: (
    sendFn: (
      method: string,
      params: unknown,
      callback?: ((resp: JsonRpcResponse) => void) | undefined,
    ) => void,
  ) => void;
}

export const generateMacroId = () => {
  return Math.random().toString(36).substring(2, 9);
};

export const useMacrosStore = create<MacrosState>((set, get) => ({
  macros: [],
  loading: false,
  initialized: false,
  sendFn: null,

  setSendFn: sendFn => {
    set({ sendFn });
  },

  loadMacros: async () => {
    if (get().initialized) return;

    const { sendFn } = get();
    if (!sendFn) {
      console.warn("JSON-RPC send function not available.");
      return;
    }

    set({ loading: true });

    try {
      await new Promise<void>((resolve, reject) => {
        sendFn("getKeyboardMacros", {}, response => {
          if (response.error) {
            console.error("Error loading macros:", response.error);
            reject(new Error(response.error.message));
            return;
          }

          const macros = (response.result as KeySequence[]) || [];

          const sortedMacros = [...macros].sort((a, b) => {
            if (a.sortOrder !== undefined && b.sortOrder !== undefined) {
              return a.sortOrder - b.sortOrder;
            }
            if (a.sortOrder !== undefined) return -1;
            if (b.sortOrder !== undefined) return 1;
            return 0;
          });

          set({
            macros: sortedMacros,
            initialized: true,
          });

          resolve();
        });
      });
    } catch (error) {
      console.error("Failed to load macros:", error);
    } finally {
      set({ loading: false });
    }
  },

  saveMacros: async (macros: KeySequence[]) => {
    const { sendFn } = get();
    if (!sendFn) {
      console.warn("JSON-RPC send function not available.");
      throw new Error("JSON-RPC send function not available");
    }

    if (macros.length > MAX_TOTAL_MACROS) {
      console.error(`Cannot save: exceeded maximum of ${MAX_TOTAL_MACROS} macros`);
      throw new Error(`Cannot save: exceeded maximum of ${MAX_TOTAL_MACROS} macros`);
    }

    for (const macro of macros) {
      if (macro.steps.length > MAX_STEPS_PER_MACRO) {
        console.error(
          `Cannot save: macro "${macro.name}" exceeds maximum of ${MAX_STEPS_PER_MACRO} steps`,
        );
        throw new Error(
          `Cannot save: macro "${macro.name}" exceeds maximum of ${MAX_STEPS_PER_MACRO} steps`,
        );
      }

      for (let i = 0; i < macro.steps.length; i++) {
        const step = macro.steps[i];
        if (step.keys && step.keys.length > MAX_KEYS_PER_STEP) {
          console.error(
            `Cannot save: macro "${macro.name}" step ${i + 1} exceeds maximum of ${MAX_KEYS_PER_STEP} keys`,
          );
          throw new Error(
            `Cannot save: macro "${macro.name}" step ${i + 1} exceeds maximum of ${MAX_KEYS_PER_STEP} keys`,
          );
        }
      }
    }

    set({ loading: true });

    try {
      const macrosWithSortOrder = macros.map((macro, index) => ({
        ...macro,
        sortOrder: macro.sortOrder !== undefined ? macro.sortOrder : index,
      }));

      const response = await new Promise<JsonRpcResponse>(resolve => {
        sendFn(
          "setKeyboardMacros",
          { params: { macros: macrosWithSortOrder } },
          response => {
            resolve(response);
          },
        );
      });

      if (response.error) {
        console.error("Error saving macros:", response.error);
        const errorMessage =
          typeof response.error.data === "string"
            ? response.error.data
            : response.error.message || "Failed to save macros";
        throw new Error(errorMessage);
      }

      // Only update the store if the request was successful
      set({ macros: macrosWithSortOrder });
    } catch (error) {
      console.error("Failed to save macros:", error);
      throw error;
    } finally {
      set({ loading: false });
    }
  },
}));

// The device exposes every overlay network through its own RPC: TailScale and
// ZeroTier answer with a settings object, NetBird with a status object, and the
// rest with nothing but `{ running: bool }`. Rather than mirror six
// near-identical field triplets in this store, the flag-only tunnels share one
// generic map that the Remote drawer refreshes while it is open.
export type TunnelKey = "netbird" | "wireguard" | "easytier" | "vnt" | "frpc" | "cloudflared";

export interface TunnelEntry {
  up: boolean;
  /** Same vocabulary as STATE_LABEL in RemoteStatusPanel.tsx. */
  state: string;
  /** Address or reason worth showing instead of the state label. */
  detail?: string;
}

export interface VpnState {
  tailScaleConnectionState: "connecting" | "connected" | "disconnected" | "closed" | "logined";
  setTailScaleConnectionState: (state: VpnState["tailScaleConnectionState"]) => void;
  
  tailScaleLoginUrl: string | null;
  setTailScaleLoginUrl: (url: string) => void;
  
  tailScaleXEdge: boolean;
  setTailScaleXEdge: (xEdge: boolean) => void;

  tailScaleIP: string | null;
  setTailScaleIP: (ip: string) => void;
  
  zeroTierConnectionState: "connecting" | "connected" | "disconnected" | "closed" | "logined";
  setZeroTierConnectionState: (state: VpnState["zeroTierConnectionState"]) => void;
 
  zeroTierNetworkID: string | null;
  setZeroTierNetworkID: (networkID: string) => void;
  
  zeroTierIP: string | null;
  setZeroTierIP: (ip: string) => void;

  tunnelEntries: Partial<Record<TunnelKey, TunnelEntry>>;
  setTunnelEntry: (key: TunnelKey, entry: TunnelEntry) => void;
};

export const useVpnStore = create<VpnState>(set => ({
  tailScaleConnectionState: "disconnected",
  setTailScaleConnectionState: state => set({ tailScaleConnectionState: state }),
  tailScaleLoginUrl: null,
  setTailScaleLoginUrl: url => set({ tailScaleLoginUrl: url }),
  tailScaleXEdge: false,
  setTailScaleXEdge: xEdge => set({ tailScaleXEdge: xEdge }),
  tailScaleIP: null,
  setTailScaleIP: url => set({ tailScaleIP: url }),
  
  zeroTierConnectionState: "disconnected",
  setZeroTierConnectionState: state => set({ zeroTierConnectionState: state }),
  zeroTierNetworkID: null,
  setZeroTierNetworkID: networkID => set({ zeroTierNetworkID: networkID }),
  zeroTierIP: null,
  setZeroTierIP: networkID => set({ zeroTierIP: networkID }),

  tunnelEntries: {},
  setTunnelEntry: (key, entry) =>
    set(state => ({ tunnelEntries: { ...state.tunnelEntries, [key]: entry } })),
}));

export interface MacrosSideState {
  sideTitle: string;
  setSideTitle: (title: string) => void;
};

export const useMacrosSideTitleState = create<MacrosSideState>(set => ({
  sideTitle: "Keyboard Macros",
  setSideTitle: title => set({ sideTitle: title }),
}));

export interface LogEntry {
  timestamp: string;
  level: 'log' | 'error' | 'warn' | 'info';
  message: string;
  originalArgs: any[];
}

export type BootStorageType = 'emmc' | 'sd' | 'unknown';

interface BootStorageState {
  bootStorageType: BootStorageType;
  setBootStorageType: (type: BootStorageType) => void;
}

export const useBootStorageStore = create<BootStorageState>(set => ({
  bootStorageType: 'unknown',
  setBootStorageType: type => set({ bootStorageType: type }),
}));
