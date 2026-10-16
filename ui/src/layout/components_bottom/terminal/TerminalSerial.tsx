import "react-simple-keyboard/build/css/index.css";

import { useEffect, useState } from "react";
import { useXTerm } from "react-xtermjs";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { ClipboardAddon } from "@xterm/addon-clipboard";
import { isDesktop,isMobile } from "react-device-detect";
import { FloatButton } from "antd";
import FBSvg1 from "@assets/second/float_button1.svg?react"
import FBSvg2 from "@assets/second/float_button2.svg?react"

import { AvailableTerminalTypes, useUiStore } from "@/hooks/stores";
import TerminalSerialSide from "@/layout/components_bottom/terminal/TerminalSerialSide";
import { TERMINAL_CONFIG } from "@/layout/components_bottom/terminal/common";
import Drawer from"@components/Drawer"

const isWebGl2Supported = !!document.createElement("canvas").getContext("webgl2");


function TerminalSerial({dataChannel} : {
  readonly pinned?: boolean;
  readonly dataChannel: RTCDataChannel;
  readonly type: AvailableTerminalTypes;
  checkPater?:(e: MouseEvent)=>boolean;
}) {
  const enableTerminal = "serial";
  const setDisableKeyboardFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const { instance, ref } = useXTerm({ options: TERMINAL_CONFIG });
  const [readyState, setReadyState] = useState(dataChannel.readyState);

  useEffect(() => {
    setTimeout(() => {
      setDisableKeyboardFocusTrap(true);
    }, 500);

    return () => {
      setDisableKeyboardFocusTrap(false);
    };
  }, [ref, instance, enableTerminal, setDisableKeyboardFocusTrap]);

  useEffect(() => {
    const handleOpen = () => setReadyState("open");
    const handleClose = () => setReadyState("closed");

    dataChannel.addEventListener("open", handleOpen);
    dataChannel.addEventListener("close", handleClose);

    return () => {
      dataChannel.removeEventListener("open", handleOpen);
      dataChannel.removeEventListener("close", handleClose);
    };
  }, [dataChannel]);

  useEffect(() => {
    if (!instance) return;
    if (readyState !== "open") return;

    const abortController = new AbortController();
    const binaryType = dataChannel.binaryType;
    dataChannel.addEventListener(
      "message",
      e => {
        // Handle binary data differently based on browser implementation
        // Firefox sends data as blobs, chrome sends data as arraybuffer
        const data = e.data;
        if (typeof data === "string") {
            instance.write(data);
        } else if (data instanceof ArrayBuffer) {
          instance.write(new Uint8Array(data));
        } else if (data instanceof Blob) {
          const reader = new FileReader();
          reader.onload = () => {
            if (!reader.result) return;
            instance.write(new Uint8Array(reader.result as ArrayBuffer));
          };
          reader.readAsArrayBuffer(data);
        } else {
             console.warn("TerminalSerial unknown message type", data);
        }
      },
      { signal: abortController.signal },
    );

    const onDataHandler = instance.onData(data => {
      if (dataChannel.readyState === "open") {
        try {
          dataChannel.send(data);
        } catch (e) {
          console.error("TerminalSerial failed to send data", e);
        }
      } else {
        console.warn("TerminalSerial data channel not open, cannot send data");
      }
    });

    // Send initial terminal size
    if (dataChannel.readyState === "open") {
      dataChannel.send(JSON.stringify({ rows: instance.rows, cols: instance.cols }));
    }

    return () => {
      abortController.abort();
      onDataHandler.dispose();
    };
  }, [instance, dataChannel, readyState]);

  useEffect(() => {
    if (!instance) return;

    // Load the fit addon
    const fitAddon = new FitAddon();
    instance.loadAddon(fitAddon);

    instance.loadAddon(new ClipboardAddon());
    instance.loadAddon(new Unicode11Addon());
    instance.loadAddon(new WebLinksAddon());
    instance.unicode.activeVersion = "11";

    if (isWebGl2Supported&&!isMobile) {
      const webGl2Addon = new WebglAddon();
      webGl2Addon.onContextLoss(() => webGl2Addon.dispose());
      instance.loadAddon(webGl2Addon);
    }
    fitAddon.fit();
    const handleResize = () => fitAddon.fit();

    // Handle resize event
    window.addEventListener("resize", handleResize);
    //if (enableTerminal) {
      setTimeout(handleResize, 50);
   // }

    // Auto focus terminal
    setTimeout(() => {
      instance.focus();
    }, 100);

    return () => {
      window.removeEventListener("resize", handleResize);
    };
  }, [ref, instance, enableTerminal]);
  const [open, setOpen] = useState(false);


  const showDrawer = () => {
    setOpen(open => !open);
  };

  const onClose = () => {

    setOpen(open => !open);
  };

  const clearTerminal = () => {
    instance?.reset();
  };

  return (

      <div
        onKeyDown={e => e.stopPropagation()}
        onKeyUp={e => e.stopPropagation()}
        style={{
          height: "100%",
          width: "100%",
          transform: "translateY(0px)",
          transition: "transform 500ms ease-in-out, opacity 300ms",
          pointerEvents: "auto",
          backgroundColor: "#0f172a",
          position: "relative" // Ensure relative positioning for absolute children
        }}>
        <div style={{
          height: isMobile?"100%":"calc(45vh - 36px)",
          width: "100%",
          padding: "12px",
          position: "relative" // Ensure relative positioning for absolute children inside
        }}>
          <div ref={ref} style={{ height: "100%", width: "100%" }} />
          
          <FloatButton   icon={open?<FBSvg2 />:<FBSvg1 />} type={open?"default":"primary"} style={{ insetInlineEnd: 24,top:24,backgroundColor:"transparent",position:"absolute",zIndex:1001 }} onClick={showDrawer} 
            tooltip={<div>{open ? "关闭设置" : "打开设置"}</div>}
          />
        </div>
        <Drawer
          closable={false}
          onClose={onClose}
          visible={open}
          placement={isDesktop ? "right" : "bottom"}
          width={isDesktop ? 250 : "100%"}
          style={{ position: 'absolute' }}
          getContainer={false}
        >
          <TerminalSerialSide clearTerminal={clearTerminal} />
        </Drawer>
      </div>


  );
}

export default TerminalSerial;
