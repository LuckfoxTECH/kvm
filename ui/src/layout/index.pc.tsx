import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Outlet,
  redirect,
  useLoaderData,
  useLocation,
  useOutlet,
} from "react-router-dom";
import { useInterval } from "usehooks-ts";
import { FocusTrap } from "focus-trap-react";
import useWebSocket from "react-use-websocket";
import { useReactAt } from 'i18n-auto-extractor/react';
import semver from "semver";

import {
  HidState,
  KeyboardLedState,
  NetworkState,
  UpdateState,
  useDeviceStore,
  useHidStore,
  useMountMediaStore,
  useNetworkStateStore,
  User,
  useRTCStore,
  useUiStore,
  useUpdateStore,
  useVideoStore,
  VideoState,
  useSettingsStore,
  BacklightSettings,
 useVpnStore } from "@/hooks/stores";
import { applyVideoInputState, type VideoInputStatePayload } from "@/hooks/videoInputState";
import { JsonRpcRequest, useJsonRpc, resetHttpSessionId } from "@/hooks/useJsonRpc";
import { doRpcHidHandshake } from "@/hooks/useHidRpc";
import { DEVICE_API } from "@/ui.config";
import api from "@/api";
import Modal from "@components/Modal";
import { useDeviceUiNavigation } from "@/hooks/useAppNavigation";
import {
  ConnectionFailedOverlay,
  LoadingConnectionOverlay,
  PeerConnectionDisconnectedOverlay,
} from "@components/VideoOverlay";
import { FeatureFlagProvider } from "@/providers/FeatureFlagProvider";
import notifications from "@/notifications";
import BarTop from "@/layout/core/bar_top/index";
import BottomBar from "@/layout/core/bar_bottom/index";
import { DeviceStatus } from "@routes/login_page/index";
import { LocalVersionInfo } from "@/layout/components_setting/version/VersionContent";
import Desktop from "@/layout/core/desktop/index";
import {  dark_bg_style_fun } from "@/layout/theme_color";
import SidebarContainer from "@/layout/core/bar_side";
import { BarsHotZones, useBarsAutoHide } from "@/layout/core/bar_autohide";
import { useTheme } from "@/layout/contexts/ThemeContext";


import enJSON from '../locales/en.json';
import zhJSON from '../locales/zh.json';
interface LocalLoaderResp {
  authMode: "password" | "noPassword" | null;
}

interface CloudLoaderResp {
  deviceName: string;
  user: User | null;
  iceConfig: {
    iceServers: { credential?: string; urls: string | string[]; username?: string };
  } | null;
}

export type AuthMode = "password" | "noPassword" | null;
export interface LocalDevice {
  authMode: AuthMode;
  deviceId: string;
}

interface TailScaleResponse {
  state: string;
  loginUrl: string;
  ip: string;
  xEdge: boolean;
}

interface ZeroTierResponse {
  state: string;
  networkID: string;
  ip: string;
}

const deviceLoader = async () => {
  const res = await api
    .GET(`${DEVICE_API}/device/status`)
    .then(res => res.json() as Promise<DeviceStatus>);

  if (!res.isSetup) return redirect("/mode");

  const deviceRes = await api.GET(`${DEVICE_API}/device`);
  if (deviceRes.status === 401) return redirect("/login-local");
  if (deviceRes.ok) {
    const device = (await deviceRes.json()) as LocalDevice;
    return { authMode: device.authMode };
  }

  throw new Error("Error fetching device");
};

export default function PCHome() {
  const { $at } = useReactAt();
  const loaderResp = useLoaderData() as LocalLoaderResp | CloudLoaderResp;
  // Depending on the mode, we set the appropriate variables
  const iceConfig = "iceConfig" in loaderResp ? loaderResp.iceConfig : null;

  const sidebarView = useUiStore(state => state.sidebarView);

  const setIsTurnServerInUse = useRTCStore(state => state.setTurnServerInUse);
  const peerConnection = useRTCStore(state => state.peerConnection);
  const setPeerConnectionState = useRTCStore(state => state.setPeerConnectionState);
  const peerConnectionState = useRTCStore(state => state.peerConnectionState);

  const setMediaMediaStream = useRTCStore(state => state.setMediaStream);
  const setPeerConnection = useRTCStore(state => state.setPeerConnection);
  const setDiskChannel = useRTCStore(state => state.setDiskChannel);
  const setRpcDataChannel = useRTCStore(state => state.setRpcDataChannel);
  const setRpcHidChannel = useRTCStore(state => state.setRpcHidChannel);
  const setRpcHidUnreliableChannel = useRTCStore(state => state.setRpcHidUnreliableChannel);
  const setRpcHidUnreliableNonOrderedChannel = useRTCStore(
    state => state.setRpcHidUnreliableNonOrderedChannel,
  );
  const setRpcHidProtocolVersion = useRTCStore(state => state.setRpcHidProtocolVersion);
  const setTransceiver = useRTCStore(state => state.setTransceiver);
  const setAudioTransceiver = useRTCStore(state => state.setAudioTransceiver);
  const location = useLocation();

  const isLegacySignalingEnabled = useRef(false);

  const [connectionFailed, setConnectionFailed] = useState(false);

  const forceHttp = useSettingsStore(state => state.forceHttp);
  const keyboardLedSync = useSettingsStore(state => state.keyboardLedSync);


  const { setOtaState } = useUpdateStore();
  const [isFullscreen, setIsFullscreen] = useState(0);
  const handleRequestFullscreen = async () => {
    setIsFullscreen(prevCount => prevCount + 1);
  };

  const [loadingMessage, setLoadingMessage] = useState("Connecting to device...");

  // The loading text is driven by a monotonic stage so that a late event from an
  // earlier step cannot walk the overlay back. Without this the UI used to freeze on
  // "ICE Gathering completed" even though the connection had already failed.
  const loadingStage = useRef(0);
  const setLoadingStage = useCallback((stage: number, message: string) => {
    if (stage < loadingStage.current) return;
    loadingStage.current = stage;
    setLoadingMessage(message);
  }, []);

  // True while the signaling socket is open. An offer can only reach the device over
  // that socket, so a peer connection created without it would gather ICE and then
  // wait forever for an answer that was never sent.
  const signalingReady = useRef(false);
  // Only the newest attempt may drive the UI or apply a remote answer.
  const peerConnectionAttempt = useRef(0);
  // The live peer connection. Superseded attempts are detached and closed explicitly,
  // so their callbacks cannot overwrite the state of the current attempt.
  const activePeerConnection = useRef<RTCPeerConnection | null>(null);
  // Watchdog for "the device never answers": the overlay must never spin forever.
  const connectWatchdog = useRef<number | null>(null);

  const clearConnectWatchdog = useCallback(() => {
    if (connectWatchdog.current !== null) {
      window.clearTimeout(connectWatchdog.current);
      connectWatchdog.current = null;
    }
  }, []);

  const discardPeerConnection = useCallback(() => {
    const stale = activePeerConnection.current;
    activePeerConnection.current = null;
    if (!stale) return;
    stale.onconnectionstatechange = null;
    stale.onnegotiationneeded = null;
    stale.onicecandidate = null;
    stale.onicegatheringstatechange = null;
    stale.ontrack = null;
    try {
      stale.close();
    } catch (e) {
      console.error("[peerConnection] Failed to close the old peer connection", e);
    }
  }, []);

  // Local sessions are kept in device memory only, so a reboot logs every client out
  // and the signaling socket handshake is refused with 401. A browser cannot read the
  // status of a failed WebSocket handshake, so probe the session over HTTP and go back
  // to the login page when it is gone. The full page load also tears down all stale
  // WebRTC/socket state.
  const authProbeInFlight = useRef(false);
  const lastAuthProbeAt = useRef(0);
  const redirectToLoginIfSessionExpired = useCallback(async () => {
    if (authProbeInFlight.current) return;
    if (Date.now() - lastAuthProbeAt.current < 2000) return;
    authProbeInFlight.current = true;
    lastAuthProbeAt.current = Date.now();
    try {
      const res = await api.GET(`${DEVICE_API}/device`);
      if (res.status === 401) {
        console.warn("[auth] Local session expired, redirecting to the login page");
        window.location.replace("/login-local");
      }
    } catch (e) {
      // Device still unreachable: keep retrying instead of bouncing to the login page.
      console.log("[auth] Session probe failed, device not reachable yet", e);
    } finally {
      authProbeInFlight.current = false;
    }
  }, []);

  const cleanupAndStopReconnecting = useCallback(
    function cleanupAndStopReconnecting() {
      console.log("Closing peer connection");

      clearConnectWatchdog();
      setConnectionFailed(true);
      if (peerConnection) {
        setPeerConnectionState(peerConnection.connectionState);
      }
      connectionFailedRef.current = true;

      peerConnection?.close();
      signalingAttempts.current = 0;
    },
    [clearConnectWatchdog, peerConnection, setPeerConnectionState],
  );

  // We need to track connectionFailed in a ref to avoid stale closure issues
  // This is necessary because syncRemoteSessionDescription is a callback that captures
  // the connectionFailed value at creation time, but we need the latest value
  // when the function is actually called. Without this ref, the function would use
  // a stale value of connectionFailed in some conditions.
  //
  // We still need the state variable for UI rendering, so we sync the ref with the state.
  // This pattern is a workaround for what useEvent hook would solve more elegantly
  // (which would give us a callback that always has access to latest state without re-creation).
  const connectionFailedRef = useRef(false);
  useEffect(() => {
    connectionFailedRef.current = connectionFailed;
  }, [connectionFailed]);

  const signalingAttempts = useRef(0);
  const setRemoteSessionDescription = useCallback(
    async function setRemoteSessionDescription(
      pc: RTCPeerConnection,
      remoteDescription: RTCSessionDescriptionInit,
    ) {
      if (useSettingsStore.getState().forceHttp) {
        console.log("[setRemoteSessionDescription] Skipping due to HTTP fallback/force mode");
        return;
      }

      // An answer for a connection that has already been replaced is useless and must
      // not drive the overlay of the current attempt.
      if (activePeerConnection.current !== pc) {
        console.log("[setRemoteSessionDescription] Ignoring answer for a stale connection");
        return;
      }

      setLoadingStage(5, "Setting remote description");

      try {
        await pc.setRemoteDescription(new RTCSessionDescription(remoteDescription));
        console.log("[setRemoteSessionDescription] Remote description set successfully");
        setLoadingStage(6, "Establishing secure connection...");
      } catch (error) {
        console.error(
          "[setRemoteSessionDescription] Failed to set remote description:",
          error,
        );
        cleanupAndStopReconnecting();
        return;
      }

      // Replace the interval-based check with a more reliable approach
      let attempts = 0;
      const checkInterval = setInterval(() => {
        // A newer attempt has taken over (signaling reconnect or "Try again" click).
        if (activePeerConnection.current !== pc) {
          clearInterval(checkInterval);
          return;
        }
        attempts++;

        // When vivaldi has disabled "Broadcast IP for Best WebRTC Performance", this never connects
        if (pc.sctp?.state === "connected") {
          console.log("[setRemoteSessionDescription] Remote description set");
          clearInterval(checkInterval);
          clearConnectWatchdog();
          setLoadingStage(7, "Connection established");
        } else if (attempts >= 10) {
          console.log(
            "[setRemoteSessionDescription] Failed to establish connection after 10 attempts",
            {
              connectionState: pc.connectionState,
              iceConnectionState: pc.iceConnectionState,
            },
          );
          cleanupAndStopReconnecting();
          clearInterval(checkInterval);
        } else {
          console.log("[setRemoteSessionDescription] Waiting for connection, state:", {
            connectionState: pc.connectionState,
            iceConnectionState: pc.iceConnectionState,
          });
        }
      }, 1000);
    },
    [cleanupAndStopReconnecting, clearConnectWatchdog, setLoadingStage],
  );

  const ignoreOffer = useRef(false);
  const isSettingRemoteAnswerPending = useRef(false);
  const makingOffer = useRef(false);

  const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";

  const { sendMessage, getWebSocket } = useWebSocket(
    //`${wsProtocol}//${window.location.host}/webrtc/signaling/client?id=${params.id}`,
    `${wsProtocol}//${window.location.host}/webrtc/signaling/client`,
    {
      heartbeat: true,
      retryOnError: true,
      reconnectAttempts: 30,
      reconnectInterval: 1000,
      onReconnectStop: () => {
        console.log("Reconnect stopped");
        cleanupAndStopReconnecting();
        // The device may be back while the session it restarted from is gone.
        void redirectToLoginIfSessionExpired();
      },

      shouldReconnect(event) {
        console.log("[Websocket] shouldReconnect", event);
        // TODO: Why true?
        return true;
      },

      onClose(event) {
        console.log("[Websocket] onClose", event);
        // We don't want to close everything down, we wait for the reconnect to stop instead
        signalingReady.current = false;
        // A 401 handshake rejection looks like a plain close here, so ask over HTTP.
        void redirectToLoginIfSessionExpired();
      },

      onError(event) {
        console.log("[Websocket] onError", event);
        // We don't want to close everything down, we wait for the reconnect to stop instead
      },
      onOpen() {
        console.log("[Websocket] onOpen");
        signalingReady.current = true;
      },

      onMessage: message => {
        if (message.data === "pong") return;

        /*
          Currently the signaling process is as follows:
            After open, the other side will send a `device-metadata` message with the device version
            If the device version is not set, we can assume the device is using the legacy signaling
            Otherwise, we can assume the device is using the new signaling

            If the device is using the legacy signaling, we close the websocket connection
            and use the legacy HTTPSignaling function to get the remote session description

            If the device is using the new signaling, we don't need to do anything special, but continue to use the websocket connection
            to chat with the other peer about the connection
        */

        const parsedMessage = JSON.parse(message.data);
        if (parsedMessage.type === "device-metadata") {
          const { deviceVersion } = parsedMessage.data;
          console.log("[Websocket] Received device-metadata message");
          console.log("[Websocket] Device version", deviceVersion);
          // If the device version is not set, we can assume the device is using the legacy signaling
          if (!deviceVersion) {
            console.log("[Websocket] Device is using legacy signaling");

            // Now we don't need the websocket connection anymore, as we've established that we need to use the legacy signaling
            // which does everything over HTTP(at least from the perspective of the client)
            isLegacySignalingEnabled.current = true;
            getWebSocket()?.close();
          } else {
            console.log("[Websocket] Device is using new signaling");
            isLegacySignalingEnabled.current = false;
          }
          setupPeerConnection();
        }

        if (!peerConnection) return;
        if (parsedMessage.type === "answer") {
          console.log("[Websocket] Received answer");
          const readyForOffer =
            // If we're making an offer, we don't want to accept an answer
            !makingOffer &&
            // If the peer connection is stable or we're SettingsModal the remote answer pending, we're ready for an offer
            (peerConnection?.signalingState === "stable" ||
              isSettingRemoteAnswerPending.current);

          // If we're not ready for an offer, we don't want to accept an offer
          ignoreOffer.current = parsedMessage.type === "offer" && !readyForOffer;
          if (ignoreOffer.current) return;

          // Set so we don't accept an answer while we're SettingsModal the remote description
          isSettingRemoteAnswerPending.current = parsedMessage.type === "answer";
          console.log(
            "[Websocket] Setting remote answer pending",
            isSettingRemoteAnswerPending.current,
          );

          const sd = atob(parsedMessage.data);
          const remoteSessionDescription = JSON.parse(sd);

          setRemoteSessionDescription(
            peerConnection,
            new RTCSessionDescription(remoteSessionDescription),
          );

          // Reset the remote answer pending flag
          isSettingRemoteAnswerPending.current = false;
        } else if (parsedMessage.type === "new-ice-candidate") {
          console.log("[Websocket] Received new-ice-candidate");
          const candidate = parsedMessage.data;
          peerConnection.addIceCandidate(candidate);
        }
      },
    },

    // Don't even retry once we declare failure
    !connectionFailed && isLegacySignalingEnabled.current === false,
  );

  const sendWebRTCSignal = useCallback(
    (type: string, data: unknown) => {
      // Second argument tells the library not to queue the message, and send it once the connection is established again.
      // We have event handlers that handle the connection set up, so we don't need to queue the message.
      // Dropping the signal silently used to leave the UI waiting for an answer that was never sent.
      const socket = getWebSocket();
      if (!socket || socket.readyState !== WebSocket.OPEN) {
        console.warn(`[Websocket] Dropping "${type}" signal, signaling channel is not open`);
        return false;
      }
      sendMessage(JSON.stringify({ type, data }), false);
      return true;
    },
    [getWebSocket, sendMessage],
  );

  const setupPeerConnection = useCallback(async () => {
    if (useSettingsStore.getState().forceHttp) {
      console.log("[setupPeerConnection] Skipping due to HTTP fallback/force mode");
      return;
    }

    const attempt = ++peerConnectionAttempt.current;

    // An offer can only reach the device over the signaling socket. Without it a new
    // peer connection would just gather candidates and then sit on "ICE Gathering
    // completed" forever, so wait for the socket instead: every newly opened socket
    // answers with `device-metadata`, which calls this function again.
    if (!isLegacySignalingEnabled.current && !signalingReady.current) {
      console.log("[setupPeerConnection] Signaling channel is closed, waiting for it");
      clearConnectWatchdog();
      discardPeerConnection();
      setPeerConnection(null);
      setConnectionFailed(false);
      setPeerConnectionState("connecting");
      loadingStage.current = 0;
      setLoadingStage(1, "Waiting for the device to reconnect...");
      return;
    }

    console.log("[setupPeerConnection] Setting up peer connection");
    // Never let two peer connections exist at the same time: the stale one would keep
    // overwriting the loading message and swallow the answer of the current attempt.
    discardPeerConnection();
    setPeerConnection(null);
    setConnectionFailed(false);
    loadingStage.current = 0;
    setLoadingStage(0, "Connecting to device...");

    let pc: RTCPeerConnection;
    try {
      console.log("[setupPeerConnection] Creating peer connection");
      setLoadingStage(1, "Creating peer connection...");
      let fetchedIceServers: RTCIceServer[] = [];
      if (!iceConfig?.iceServers) {
        try {
          const res = await api.GET("/api/ice-servers");
          const data = await res.json();
          fetchedIceServers = data.iceServers ?? [];
        } catch (e) {
          console.error("failed to fetch ICE servers, fallback", e);
          fetchedIceServers = [{ urls: ["stun:stun.l.google.com:19302"] }];
        }
      }

      pc = new RTCPeerConnection({
        // We only use STUN or TURN servers if we're in the cloud
        //...(isInCloud && iceConfig?.iceServers
        //  ? { iceServers: [iceConfig?.iceServers] }
        //  : {}),
        ...(iceConfig?.iceServers
          ? { iceServers: [iceConfig?.iceServers] }
          : { iceServers: fetchedIceServers }),
      });

      setPeerConnectionState(pc.connectionState);
      console.log("[setupPeerConnection] Peer connection created", pc);
      setLoadingStage(2, "Setting up connection to device...");
    } catch (e) {
      console.error(`[setupPeerConnection] Error creating peer connection: ${e}`);
      setTimeout(() => {
        cleanupAndStopReconnecting();
      }, 1000);
      return;
    }

    // A newer attempt started while we were awaiting the ICE servers: drop this one
    // instead of registering it, otherwise it would silently take over the UI and
    // strand the newer attempt without a watchdog.
    if (peerConnectionAttempt.current !== attempt) {
      console.log("[setupPeerConnection] Superseded by a newer attempt, discarding");
      try {
        pc.close();
      } catch (e) {
        console.error("[setupPeerConnection] Failed to close the abandoned connection", e);
      }
      return;
    }

    activePeerConnection.current = pc;

    // Never leave the overlay spinning: if the device does not answer in time, fall
    // back to the explicit "Connection Error" state where the user can click retry.
    clearConnectWatchdog();
    connectWatchdog.current = window.setTimeout(() => {
      if (activePeerConnection.current !== pc) return;
      if (pc.connectionState === "connected") return;
      console.warn("[setupPeerConnection] Timed out waiting for the device, giving up");
      cleanupAndStopReconnecting();
    }, 20000);

    // Set up event listeners and data channels
    pc.onconnectionstatechange = () => {
      // A newer attempt owns the UI now.
      if (peerConnectionAttempt.current !== attempt) return;
      console.log("[setupPeerConnection] Connection state changed", pc.connectionState);
      if (pc.connectionState === "connected") clearConnectWatchdog();
      setPeerConnectionState(pc.connectionState);
    };

    pc.onnegotiationneeded = async () => {
      try {
        console.log("[setupPeerConnection] Creating offer");
        makingOffer.current = true;

        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        // Another attempt took over while we were awaiting, do not publish this offer.
        if (peerConnectionAttempt.current !== attempt) return;
        const sd = btoa(JSON.stringify(pc.localDescription));
        const isNewSignalingEnabled = isLegacySignalingEnabled.current === false;
        if (isNewSignalingEnabled) {
          sendWebRTCSignal("offer", { sd: sd });
        } else {
          console.log("Legacy signanling. Waiting for ICE Gathering to complete...");
        }
      } catch (e) {
        console.error(
          `[setupPeerConnection] Error creating offer: ${e}`,
          new Date().toISOString(),
        );
        cleanupAndStopReconnecting();
      } finally {
        makingOffer.current = false;
      }
    };

    pc.onicecandidate = async ({ candidate }) => {
      if (!candidate) return;
      if (candidate.candidate === "") return;
      if (activePeerConnection.current !== pc) return;
      sendWebRTCSignal("new-ice-candidate", candidate);
    };

    pc.onicegatheringstatechange = event => {
      const pc = event.currentTarget as RTCPeerConnection;
      if (activePeerConnection.current !== pc) return;
      if (pc.iceGatheringState === "complete") {
        console.log("ICE Gathering completed");
        setLoadingStage(4, "ICE Gathering completed");
      } else if (pc.iceGatheringState === "gathering") {
        console.log("ICE Gathering Started");
        setLoadingStage(3, "Gathering ICE candidates...");
      }
    };

    pc.ontrack = function (event) {
      setMediaMediaStream(event.streams[0]);
    };

    setTransceiver(pc.addTransceiver("video", { direction: "recvonly" }));
    pc.addTransceiver("audio", { direction: "recvonly" });

    const rpcDataChannel = pc.createDataChannel("rpc");
    rpcDataChannel.onopen = () => {
      setRpcDataChannel(rpcDataChannel);
    };

    // Binary HID RPC channels. The ordered channel carries key and macro
    // reports; the unreliable ones carry pointer motion, where a dropped
    // sample is better than a stale one delivered late.
    const rpcHidChannel = pc.createDataChannel("hidrpc");
    rpcHidChannel.binaryType = "arraybuffer";
    rpcHidChannel.onopen = () => {
      setRpcHidChannel(rpcHidChannel);
    };
    doRpcHidHandshake(rpcHidChannel, setRpcHidProtocolVersion);

    const rpcHidUnreliableChannel = pc.createDataChannel("hidrpc-unreliable-ordered", {
      ordered: true,
      maxRetransmits: 0,
    });
    rpcHidUnreliableChannel.binaryType = "arraybuffer";
    rpcHidUnreliableChannel.onopen = () => {
      setRpcHidUnreliableChannel(rpcHidUnreliableChannel);
    };

    const rpcHidUnreliableNonOrderedChannel = pc.createDataChannel(
      "hidrpc-unreliable-nonordered",
      {
        ordered: false,
        maxRetransmits: 0,
      },
    );
    rpcHidUnreliableNonOrderedChannel.binaryType = "arraybuffer";
    rpcHidUnreliableNonOrderedChannel.onopen = () => {
      setRpcHidUnreliableNonOrderedChannel(rpcHidUnreliableNonOrderedChannel);
    };

    const diskDataChannel = pc.createDataChannel("disk");
    diskDataChannel.onopen = () => {
      setDiskChannel(diskDataChannel);
    };

    setPeerConnection(pc);
  }, [
    forceHttp,
    cleanupAndStopReconnecting,
    clearConnectWatchdog,
    discardPeerConnection,
    setLoadingStage,
    iceConfig?.iceServers,
    sendWebRTCSignal,
    setDiskChannel,
    setMediaMediaStream,
    setPeerConnection,
    setPeerConnectionState,
    setRpcDataChannel,
    setRpcHidChannel,
    setRpcHidUnreliableChannel,
    setRpcHidUnreliableNonOrderedChannel,
    setRpcHidProtocolVersion,
    setTransceiver,
    setAudioTransceiver,
  ]);

  useEffect(() => {
    if (peerConnectionState === "failed") {
      console.log("Connection failed, closing peer connection");
      cleanupAndStopReconnecting();
    }
  }, [peerConnectionState, cleanupAndStopReconnecting]);

  // Cleanup effect
  const clearInboundRtpStats = useRTCStore(state => state.clearInboundRtpStats);
  const clearCandidatePairStats = useRTCStore(state => state.clearCandidatePairStats);
  const setSidebarView = useUiStore(state => state.setSidebarView);

  useEffect(() => {
    return () => {
      clearConnectWatchdog();
      peerConnection?.close();
    };
  }, [peerConnection, clearConnectWatchdog]);

  // For some reason, we have to have this unmount separate from the cleanup effect above
  useEffect(() => {
    return () => {
      clearInboundRtpStats();
      clearCandidatePairStats();
      setSidebarView(null);
      setPeerConnection(null);
    };
  }, [clearCandidatePairStats, clearInboundRtpStats, setPeerConnection, setSidebarView]);

  // TURN server usage detection
  useEffect(() => {
    if (peerConnectionState !== "connected") return;
    const { localCandidateStats, remoteCandidateStats } = useRTCStore.getState();

    const lastLocalStat = Array.from(localCandidateStats).pop();
    if (!lastLocalStat?.length) return;
    const localCandidateIsUsingTurn = lastLocalStat[1].candidateType === "relay"; // [0] is the timestamp, which we don't care about here

    const lastRemoteStat = Array.from(remoteCandidateStats).pop();
    if (!lastRemoteStat?.length) return;
    const remoteCandidateIsUsingTurn = lastRemoteStat[1].candidateType === "relay"; // [0] is the timestamp, which we don't care about here

    setIsTurnServerInUse(localCandidateIsUsingTurn || remoteCandidateIsUsingTurn);
  }, [peerConnectionState, setIsTurnServerInUse]);

  // Vpn State Update
  const tailScaleConnectionState = useVpnStore(state => state.tailScaleConnectionState);
  const setTailScaleConnectionState = useVpnStore(state => state.setTailScaleConnectionState);
  const setTailScaleXEdge = useVpnStore(state => state.setTailScaleXEdge);
  const setTailScaleLoginUrl = useVpnStore(state => state.setTailScaleLoginUrl);
  const setTailScaleIP = useVpnStore(state => state.setTailScaleIP);
  const zeroTierConnectionState = useVpnStore(state => state.zeroTierConnectionState);

  const setZeroTierConnectionState = useVpnStore(state => state.setZeroTierConnectionState);
  const setZeroTierNetworkID = useVpnStore(state => state.setZeroTierNetworkID);
  const setZeroTierIP = useVpnStore(state => state.setZeroTierIP);

  const setNetworkState = useNetworkStateStore(state => state.setNetworkState);

  const setUsbState = useHidStore(state => state.setUsbState);
  const setHdmiState = useVideoStore(state => state.setHdmiState);
  const setVideoInput = useVideoStore(state => state.setVideoInput);

  const setKeyboardLedState = useHidStore(state => state.setKeyboardLedState);
  const setKeysDownState = useHidStore(state => state.setKeysDownState);

  const setKeyboardLedStateSyncAvailable = useHidStore(state => state.setKeyboardLedStateSyncAvailable);

  const [hasUpdated, setHasUpdated] = useState(false);
  const [sessionInvalidated, setSessionInvalidated] = useState(false);
  const { navigateTo } = useDeviceUiNavigation();

  function onJsonRpcRequest(resp: JsonRpcRequest) {
    if (resp.method === "otherSessionConnected") {
      navigateTo("/other-session");
    }

    if (resp.method === "sessionInvalidated") {
      resetHttpSessionId();
      setSessionInvalidated(true);
      return;
    }

    if (resp.method === "usbState") {
      setUsbState(resp.params as unknown as HidState["usbState"]);
    }

    if (resp.method === "videoInputState") {
      applyVideoInputState(resp.params as VideoInputStatePayload, setHdmiState, setVideoInput);
    }

    if (resp.method === "networkState") {
      console.log("Setting network state", resp.params);
      setNetworkState(resp.params as NetworkState);
    }

    if (resp.method === "backlightChanged") {
      // The touchscreen slider changed the brightness; mirror it here so an open
      // settings page does not show a stale value.
      useSettingsStore
        .getState()
        .setBacklightSettings({
          ...useSettingsStore.getState().backlightSettings,
          max_brightness: (resp.params as Pick<BacklightSettings, "max_brightness">)
            .max_brightness,
        });
    }

    if (resp.method === "displayState") {
      // The panel was switched on/off, possibly by the touchscreen.
      useSettingsStore
        .getState()
        .setDisplayOn((resp.params as { on: boolean }).on);
    }

    if (resp.method === "keyboardLedState") {
      const ledState = resp.params as KeyboardLedState;
      console.log("Setting keyboard led state", ledState);
      setKeyboardLedState(ledState);
      setKeyboardLedStateSyncAvailable(true);
    }

    if (resp.method === "keysDownState") {
      // Device-truth key state: the source for per-side modifier highlighting.
      setKeysDownState(resp.params as { modifier: number; keys: number[] });
    }

    if (resp.method === "otaState") {
      const otaState = resp.params as UpdateState["otaState"];
      setOtaState(otaState);

      if (otaState.updating === true) {
        setHasUpdated(true);
      }
    }
  }

  const rpcDataChannel = useRTCStore(state => state.rpcDataChannel);
  const [send] = useJsonRpc(onJsonRpcRequest);

  const updateVpnStates = useCallback(() => {
    // TailScaleState
    send("getTailScaleSettings", {}, resp => {
      if ("error" in resp) return;
      const result = resp.result as TailScaleResponse;
      const validState = ["closed", "connecting", "connected", "disconnected", "logined"].includes(result.state)
        ? result.state as "closed" | "connecting" | "connected" | "disconnected" | "logined"
        : "closed";

      setTailScaleXEdge(result.xEdge);
      setTailScaleConnectionState(validState);
      setTailScaleLoginUrl(result.loginUrl);
      setTailScaleIP(result.ip);
    });

    // ZeroTier
    if (zeroTierConnectionState !== "connecting" && zeroTierConnectionState !== "closed") {
      send("getZeroTierSettings", {}, resp => {
        if ("error" in resp) return;
        const result = resp.result as ZeroTierResponse;
        const validState = ["closed", "connecting", "connected", "disconnected", "logined"].includes(result.state)
          ? result.state as "closed" | "connecting" | "connected" | "disconnected" | "logined"
          : "closed";
        setZeroTierConnectionState(validState);
        setZeroTierNetworkID(result.networkID);
        setZeroTierIP(result.ip);
      });
    }
  }, [
    send,
    setTailScaleConnectionState,
    setTailScaleIP,
    setTailScaleLoginUrl,
    setTailScaleXEdge,
    setZeroTierConnectionState,
    setZeroTierIP,
    setZeroTierNetworkID,
    zeroTierConnectionState,
  ]);

  useEffect(() => {
    updateVpnStates();
  }, [updateVpnStates]);
  useInterval(updateVpnStates, 5000);

  const updateUsbState = useCallback(() => {
    send("getUSBState", {}, resp => {
      if ("error" in resp) return;
      setUsbState(resp.result as HidState["usbState"]);
    });
  }, [send, setUsbState]);

  const updateVideoState = useCallback(() => {
    send("getVideoState", {}, resp => {
      if ("error" in resp) return;
      applyVideoInputState(resp.result as VideoInputStatePayload, setHdmiState, setVideoInput);
    });
  }, [send, setHdmiState, setVideoInput]);

  useEffect(() => {
    if (rpcDataChannel?.readyState !== "open") return;
    updateVideoState();
    updateVpnStates();
  }, [rpcDataChannel?.readyState, updateVideoState]);

  useEffect(() => {
    if (!forceHttp) return;
    updateVideoState();
    updateUsbState();
  }, [forceHttp, updateUsbState, updateVideoState]);

  useInterval(() => {
    updateVideoState();
    updateUsbState();
  }, forceHttp ? 1000 : null);

  // request keyboard led state from the device
  useEffect(() => {
    if (!forceHttp && rpcDataChannel?.readyState !== "open") return;
    // Only host mode reads the device. In browser mode asking here would put the
    // device's (often unknown) values on top of the browser's own readings.
    if (keyboardLedSync !== "host") return;
    // Query state belongs to this connection and LED source, not the component.
    let disposed = false;
    let pending = false;
    let received = false;
    let disabled = false;
    let generation = 0;
    let timeout: ReturnType<typeof setTimeout> | undefined;

    // The device only pushes a new state when the host changes its lock lights,
    // and the host may not have reported yet when the channel opens. Retry until
    // the first answer arrives instead of leaving the panel blank forever.
    const request = () => {
      if (disposed || disabled || pending || received) return;
      pending = true;
      const requestGeneration = ++generation;
      timeout = setTimeout(() => {
        generation++;
        pending = false;
      }, 2000);
      send("getKeyboardLedState", {}, resp => {
        if (disposed || generation !== requestGeneration ||
            useSettingsStore.getState().keyboardLedSync !== "host") return;
        clearTimeout(timeout);
        pending = false;
        if ("error" in resp) {
          // -32601 means the method is not supported
          if (resp.error.code === -32601) {
            disabled = true;
            setKeyboardLedStateSyncAvailable(false);
            console.error("Failed to get keyboard led state, disabling sync", resp.error);
          } else {
            console.error("Failed to get keyboard led state, will retry", resp.error);
          }
          return;
        }
        console.log("Keyboard led state", resp.result);
        received = (resp.result as KeyboardLedState).reported !== false;
        setKeyboardLedState(resp.result as KeyboardLedState);
        setKeyboardLedStateSyncAvailable(true);
      });
    };

    request();
    const timer = setInterval(request, 2000);
    return () => {
      disposed = true;
      clearTimeout(timeout);
      clearInterval(timer);
    };
  }, [rpcDataChannel, forceHttp, keyboardLedSync, send, setKeyboardLedState, setKeyboardLedStateSyncAvailable]);

  const diskChannel = useRTCStore(state => state.diskChannel)!;
  const file = useMountMediaStore(state => state.localFile)!;
  useEffect(() => {
    if (!diskChannel || !file) return;
    diskChannel.onmessage = async e => {
      console.log("Received", e.data);
      const data = JSON.parse(e.data);
      const blob = file.slice(data.start, data.end);
      const buf = await blob.arrayBuffer();
      const header = new ArrayBuffer(16);
      const headerView = new DataView(header);
      headerView.setBigUint64(0, BigInt(data.start), false); // start offset, big-endian
      headerView.setBigUint64(8, BigInt(buf.byteLength), false); // length, big-endian
      const fullData = new Uint8Array(header.byteLength + buf.byteLength);
      fullData.set(new Uint8Array(header), 0);
      fullData.set(new Uint8Array(buf), header.byteLength);
      diskChannel.send(fullData);
    };
  }, [diskChannel, file]);

  // System update
  const disableKeyboardFocusTrap = useUiStore(state => state.disableVideoFocusTrap);

  // const [kvmTerminal, setKvmTerminal] = useState<RTCDataChannel | null>(null);
  // const [serialConsole, setSerialConsole] = useState<RTCDataChannel | null>(null);



  const outlet = useOutlet();
  const onModalClose = useCallback(() => {
    if (location.pathname !== "/other-session") navigateTo("/");
  }, [navigateTo, location.pathname]);

  const appVersion = useDeviceStore(state => state.appVersion);
  const systemVersion = useDeviceStore(state => state.systemVersion);
  const setAppVersion = useDeviceStore(state => state.setAppVersion);
  const setSystemVersion = useDeviceStore(state => state.setSystemVersion);
  const [lowSystemVersionPromptDismissed, setLowSystemVersionPromptDismissed] = useState(false);

  useEffect(() => {
    if (appVersion && systemVersion) return;

    send("getLocalUpdateStatus", {}, async resp => {
      if ("error" in resp) {
        notifications.error(`Failed to get device version: ${resp.error}`);
        return
      }

      const result = resp.result as LocalVersionInfo;
      setAppVersion(result.appVersion);
      setSystemVersion(result.systemVersion);
    });
  }, [appVersion, send, setAppVersion, setSystemVersion, systemVersion]);

  const isSystemVersionTooLow = useMemo(() => {
    const baseCurrentVersion = semver.coerce(systemVersion ?? "")?.version;
    const baseMinVersion = semver.coerce("0.1.4")?.version;
    if (!baseCurrentVersion || !baseMinVersion) return false;
    return semver.lt(baseCurrentVersion, baseMinVersion);
  }, [systemVersion]);

  const ConnectionStatusElement = useMemo(() => {
    const hasConnectionFailed =
      connectionFailed || ["failed", "closed"].includes(peerConnectionState ?? "");

    const isPeerConnectionLoading =
      ["connecting", "new"].includes(peerConnectionState ?? "") ||
      peerConnection === null;

    const isDisconnected = peerConnectionState === "disconnected" && !forceHttp;

    const isOtherSession = location.pathname.includes("other-session");

    if (isOtherSession) return null;
    if (peerConnectionState === "connected") return null;
    if (isDisconnected) {
      return <PeerConnectionDisconnectedOverlay show={true} />;
    }

    if (hasConnectionFailed)
      return (
        <ConnectionFailedOverlay show={true} setupPeerConnection={setupPeerConnection} />
      );
    if (forceHttp) return null;

    if (isPeerConnectionLoading) {
      return <LoadingConnectionOverlay show={true} text={loadingMessage} />;
    }

    return null;
  }, [
    connectionFailed,
    loadingMessage,
    location.pathname,
    peerConnection,
    peerConnectionState,
    setupPeerConnection,
  ]);

  const { isDark } = useTheme();
  const { barsVisible, showBars, revealBars, scheduleHideBars } = useBarsAutoHide();

  const language = useSettingsStore(state => state.language);
  const { setCurrentLang } = useReactAt();
  // Initialize Language
  useEffect(() => {
    setCurrentLang(language, language === 'en' ? enJSON : zhJSON);
  }, [language, setCurrentLang]);

  return (
    <FeatureFlagProvider appVersion={appVersion}>

      <div className="relative h-full overflow-hidden">
        {!lowSystemVersionPromptDismissed && isSystemVersionTooLow && (
          <div className="absolute inset-0 z-[19999] flex items-center justify-center bg-black/60">
            <div className="rounded-md bg-white px-6 py-4 text-center shadow-lg dark:bg-[#1a1a1a]">
              <p className="mb-2 text-base font-semibold text-slate-900 dark:text-white">
                {$at("Your system version is outdated (< 0.1.4)")}
              </p>
              <p className="text-sm text-slate-600 dark:text-slate-300">
                {$at("Please upgrade to the latest firmware as soon as possible.")}
              </p>
              <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                {$at("Current system version")}: {systemVersion}
              </p>
              <button
                className="mt-4 rounded bg-[rgba(22,152,217,1)] dark:bg-[rgba(45,106,229,1)] px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
                onClick={() => setLowSystemVersionPromptDismissed(true)}
              >
                {$at("I understand")}
              </button>
            </div>
          </div>
        )}
        {sessionInvalidated && (
          <div className="absolute inset-0 z-[20000] flex items-center justify-center bg-black/60">
            <div className="rounded-md bg-white px-6 py-4 text-center shadow-lg dark:bg-slate-800">
              <p className="mb-2 text-base font-semibold text-slate-900 dark:text-white">
                {$at("The current page has been launched")}
              </p>
              <p className="text-sm text-slate-600 dark:text-slate-300">
                {$at("Please close this page or continue using the device in a new page.")}
              </p>
            </div>
          </div>
        )}
        <FocusTrap
          paused={disableKeyboardFocusTrap}
          focusTrapOptions={{
            allowOutsideClick: true,
            escapeDeactivates: false,
            fallbackFocus: "#videoFocusTrap",
          }}
        >
          <div className="absolute top-0">
            <button className="absolute top-0  bg-fuchsia-300" tabIndex={-1} id="videoFocusTrap" />
          </div>
        </FocusTrap>

        <BarsHotZones
          barsVisible={barsVisible}
          showBars={showBars}
          revealBars={revealBars}
          scheduleHideBars={scheduleHideBars}
        />
        <div className={`grid h-full grid-rows-(--grid-headerBody) select-none ${dark_bg_style_fun(isDark)}`}>
          <div
            onMouseEnter={showBars}
            onMouseLeave={scheduleHideBars}
            className={`transition-[max-height,opacity] duration-300 ease-out ${
              barsVisible
                ? "max-h-10 overflow-visible opacity-100"
                : "max-h-0 overflow-hidden opacity-0 pointer-events-none"
            }`}
          >
            <BarTop requestFullscreen={handleRequestFullscreen} />
          </div>
          <div className="relative flex h-full w-full overflow-hidden">
            <Desktop isFullscreen={isFullscreen} />
            <div
              style={{ animationDuration: "500ms" }}
              className="animate-slideUpFade pointer-events-none absolute inset-0 z-20 flex items-center justify-center"
            >
              <div className="relative h-full w-full">
                {!!ConnectionStatusElement && ConnectionStatusElement}
                {/*<ConnectionFailedOverlay show={true} setupPeerConnection={setupPeerConnection} />*/}
              </div>
            </div>

            <SidebarContainer sidebarView={sidebarView} />
          </div>
          <div
            onMouseEnter={showBars}
            onMouseLeave={scheduleHideBars}
            className={`transition-[max-height,opacity] duration-300 ease-out ${
              barsVisible
                ? "max-h-10 overflow-visible opacity-100"
                : "max-h-0 overflow-hidden opacity-0 pointer-events-none"
            }`}
          >
            <BottomBar />
          </div>
        </div>
      </div>

      <div
        className="z-50"
        onClick={e => e.stopPropagation()}
        onMouseUp={e => e.stopPropagation()}
        onMouseDown={e => e.stopPropagation()}
        onKeyUp={e => e.stopPropagation()}
        onKeyDown={e => {
          e.stopPropagation();
          if (e.key === "Escape") navigateTo("/");
        }}
      >
        <Modal open={outlet !== null} onClose={onModalClose}>
          {/* The 'used by other session' modal needs to have access to the connectWebRTC function */}
          <Outlet context={{ setupPeerConnection }} />
        </Modal>

      </div>

    </FeatureFlagProvider>
  );
}
