import React, { useCallback } from "react";
import { ExclamationTriangleIcon } from "@heroicons/react/24/solid";
import { motion, AnimatePresence } from "framer-motion";
import { LuPlay } from "react-icons/lu";
import { BsMouseFill } from "react-icons/bs";
import { Button as AntdButton, Card as AntdCard } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";
import SuaXinSvg from "@assets/second/shuaxin.svg?react";
import TiaoZhuanSvg from "@assets/second/tiaozhuan.svg?react";
import HdmiCordSvg from "@assets/second/hdmi-cord.svg?react";
import XinHaoSvg from "@assets/second/xinhao.svg?react";
import { isMobile } from "react-device-detect";

import notifications from "@/notifications";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { isKeyboardTapSuppressed } from "@/utils/keyboardTapGuard";
import Card, { GridCard } from "@components/Card";
import LoadingSpinner from "@components/LoadingSpinner";
import { Button } from "@components/Button";
import {
  dark_bd_style,
  dark_bg2_style, dark_bg_style,
  dark_font_style,
  text_primary_color,
} from "@/layout/theme_color";

interface OverlayContentProps {
  readonly children: React.ReactNode;
}

function OverlayContent({ children }: OverlayContentProps) {
  return (
    <GridCard cardClassName="h-full pointer-events-auto outline-hidden!">
      <div
        className={`flex h-full w-full bg-[rgba(248,248,248,1)] dark:bg-black flex-col items-center justify-center 
        ${isMobile?"":"rounded-md border border-slate-800/30 dark:border-slate-300/20"}`}>
        {children}
      </div>
    </GridCard>
  );
}

interface LoadingOverlayProps {
  readonly show: boolean;
}

export function LoadingVideoOverlay({ show }: LoadingOverlayProps) {
  const { $at } = useReactAt();
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{
            duration: show ? 0.3 : 0.1,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="flex flex-col items-center justify-center gap-y-1">
              <div className="animate flex h-12 w-12 items-center justify-center">
                <LoadingSpinner className="h-8 w-8 text-[rgba(22,152,217,1)] dark:text-[rgba(45,106,229,1)]" />
              </div>
              <p className="text-center text-sm text-slate-700 dark:text-slate-300">
                {$at("Loading video stream...")}
              </p>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface LoadingConnectionOverlayProps {
  readonly show: boolean;
  readonly text: string;
}

export function LoadingConnectionOverlay({ show, text }: LoadingConnectionOverlayProps) {
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="absolute inset-0 h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0 } }}
          transition={{
            duration: 0.4,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="flex flex-col items-center justify-center gap-y-1">
              <div className="animate flex h-12 w-12 items-center justify-center">
                <LoadingSpinner className="h-8 w-8 text-[rgba(22,152,217,1)] dark:text-[rgba(45,106,229,1)]" />
              </div>
              <p className="text-center text-sm text-slate-700 dark:text-slate-300">
                {text}
              </p>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface ConnectionErrorOverlayProps {
  readonly show: boolean;
  readonly setupPeerConnection: () => Promise<void>;
}

export function ConnectionFailedOverlay({
                                          show,
                                          setupPeerConnection,
                                        }: ConnectionErrorOverlayProps)
{
  const { $at } = useReactAt();
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="absolute inset-0 h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0 } }}
          transition={{
            duration: 0.4,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className={`${isMobile ? " h-full w-full justify-center p-[24px]" : "w-[38%] p-[48px]"}
               flex flex-col items-center  gap-y-1 ${dark_bg2_style} border ${dark_bd_style}`}>
              <XinHaoSvg className={`h-12 w-12 ${text_primary_color}`} />
              <h2 className={`text-xl font-bold ${dark_font_style}`}>{$at("Connection Error")}</h2>

              <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                <div className="space-y-4">

                  <div className="space-y-2 text-black dark:text-white">
                    <AntdCard className={`${dark_bg_style} ${dark_bd_style}`}>
                      <ul className="list-disc space-y-2 pl-4 text-left">

                        <li>
                          {$at("Make sure the ")}
                          <span className="text-[red]">{$at("PicoKVM ")}</span>
                          {$at("is powered on and properly connected")}

                        </li>

                        <li>{$at("Check all cables and connectors for any loose or damaged parts")}</li>

                        <li>
                          {$at("Verify that ")}
                          <span className="text-[red]">{$at("PicoKVM's ")}</span>
                          {$at("network connection is active and stable")}
                        </li>
                        <li>
                          {$at("Try restarting both the ")}
                          <span className="text-[red]">{$at("PicoKVM ")}</span>
                          {$at("and your computer")}
                        </li>
                      </ul>
                    </AntdCard>
                  </div>
                  <div className={`flex  w-full justify-between ${isMobile ? "flex-col h-[100px]" : "flex-row"}`}>
                    <AntdButton
                      type="primary"
                      // icon={<ArrowPathIcon />}
                      icon={<SuaXinSvg />}
                      iconPosition={"end"}
                      onClick={() => setupPeerConnection()}
                      className={isMobile ? "w-full !h-[40px]" : "w-[49%]"}
                    >
                      {$at("Try again")}
                    </AntdButton>

                    {isMobile && <div className="w-full h-[10px]"></div>}
                    <AntdButton
                      href={"https://wiki.luckfox.com/intro"}
                      icon={<TiaoZhuanSvg />}
                      iconPosition={"end"}
                      onClick={() => setupPeerConnection()}
                      className={isMobile ? "w-full !h-[40px]" : "w-[49%]"}
                    >
                      {$at("Troubleshooting Guide")}
                    </AntdButton>

                  </div>
                </div>
              </div>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface PeerConnectionDisconnectedOverlay {
  readonly show: boolean;
}

export function PeerConnectionDisconnectedOverlay({
                                                    show,
                                                  }: PeerConnectionDisconnectedOverlay)
{
  const { $at } = useReactAt();
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="absolute inset-0 h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0, transition: { duration: 0 } }}
          transition={{
            duration: 0.4,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className={`${isMobile ? " h-full w-full justify-center p-[24px]" : "w-[38%] p-[48px]"}
               flex flex-col items-center  gap-y-1 ${dark_bg2_style} border ${dark_bd_style}`}>
              <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
              <h2 className={`text-xl font-bold ${dark_font_style}`}>{$at("Connection Issue Detected")}</h2>
              <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                <div className="space-y-4">
                  <div className="space-y-2 text-black dark:text-white">
                    <AntdCard className={`${dark_bg_style} ${dark_bd_style}`}>
                      <ul className="list-disc space-y-2 pl-4 text-left">
                        <li>{$at("Verify that the device is powered on and properly connected")}</li>
                        <li>{$at("Check all cable connections for any loose or damaged wires")}</li>
                        <li>{$at("Ensure your network connection is stable and active")}</li>
                        <li>{$at("Try restarting both the device and your computer")}</li>
                      </ul>
                    </AntdCard>
                  </div>
                  <div className="flex items-center gap-x-2">
                    <Card>
                      <div className="flex items-center gap-x-2 p-4">
                        <LoadingSpinner className="h-4 w-4 text-[rgba(22,152,217,1)] dark:text-[rgba(45,106,229,1)]" />
                        <p className="text-sm text-slate-700 dark:text-slate-300">
                          {$at("Retrying connection...")}
                        </p>
                      </div>
                    </Card>
                  </div>
                </div>
              </div>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface HDMIErrorOverlayProps {
  readonly show: boolean;
  readonly hdmiState: string;
}

import {
  useHidStore,
} from "@/hooks/stores";

export function HDMIErrorOverlay({ show, hdmiState }: HDMIErrorOverlayProps) {
  const isNoSignal = hdmiState === "no_signal";
  const isOtherError = hdmiState === "no_lock" || hdmiState === "out_of_range";
  const { $at } = useReactAt();
  const [send] = useJsonRpc();

  const setVirtualKeyboardEnabled = useHidStore(state => state.setVirtualKeyboardEnabled);
  const isVirtualKeyboardEnabled = useHidStore(state => state.isVirtualKeyboardEnabled);
  const allowTapToOpenVirtualKeyboard = useHidStore(state => state.allowTapToOpenVirtualKeyboard);

  const handleClick = () => {
    if (isKeyboardTapSuppressed()) return;
    if (isMobile && allowTapToOpenVirtualKeyboard && !isVirtualKeyboardEnabled) {
      setVirtualKeyboardEnabled(true);
    }
  };

  const onSendUsbWakeupSignal = useCallback(() => {
    send("sendUsbWakeupSignal", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to send USB wakeup signal: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
    });
  }, [send]);

  return (
    <>
      <AnimatePresence>
        {show && isNoSignal && (
          <motion.div
            className="absolute inset-0 h-full w-full "
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{
              duration: 0.3,
              ease: "easeInOut",
            }}
          >

            <OverlayContent>
              <div className={`${isMobile?" h-full w-full justify-center p-[24px]":"w-[45%] p-[48px]"}
               flex flex-col items-center  gap-y-1 ${dark_bg2_style} border ${dark_bd_style}`}>
                <HdmiCordSvg className={`h-12 w-12 ${text_primary_color}`} />
                <h2 className={`text-xl font-bold ${dark_font_style}`}>{$at("HDMI signal not detected")}</h2>
                <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                  <div className="space-y-4">
                    <div className="space-y-2 text-black dark:text-white">


                      <AntdCard
                          onClick={handleClick}
                          onTouchEnd={handleClick}
                          className={`${dark_bg_style} ${dark_bd_style}`}
                      >
                        <ul className="list-disc space-y-2 pl-4 text-left">
                          <li>
                            {$at("Make sure the HDMI cable is securely connected between the ")}
                            <span className="text-[red]">{$at("PicoKVM ")}</span>
                            {$at("and ")}
                            <span className="text-[red]">{$at("source device")}</span>
                          </li>
                          <li>
                            {$at("If using an adapter, ensure it's compatible and functioning correctly")}
                          </li>
                          <li>
                            {$at("Confirm the  ")}
                            <span className="text-[red]">{$at("source device ")}</span>
                            {$at("is powered on and sending video output")}
                          </li>
                          <li>
                            {$at("Confirm the  ")}
                            <span className="text-[red]">{$at("source device ")}</span>
                            {$at("is awake and sending video output")}
                          </li>
                          <li>
                            {$at("Certain motherboards do not support simultaneous multi-display output")}
                          </li> 
                        </ul>
                      </AntdCard>
                    </div>
                    <div className={`flex  w-full justify-between ${isMobile ? "flex-col h-[100px]" : "flex-row"}`}>

                      <AntdButton
                        type="primary"
                        icon={<SuaXinSvg />}
                        iconPosition={"end"}
                        onClick={onSendUsbWakeupSignal}
                        className={isMobile?"w-full !h-[40px]":"w-[49%]"}
                      >
                        {$at("Try Wakeup")}
                      </AntdButton>
                      {isMobile&&<div className="w-full h-[10px]"></div>}

                      <AntdButton
                        href={"https://wiki.luckfox.com/intro"}
                        iconPosition={"end"}
                        icon={<TiaoZhuanSvg />}
                        className={isMobile?"w-full !h-[40px]":"w-[49%]"}
                      >
                        {$at("Learn more")}
                      </AntdButton>
                    </div>
                  </div>
                </div>
              </div>
            </OverlayContent>
          </motion.div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {show && isOtherError && (
          <motion.div
            className="absolute inset-0 h-full w-full"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{
              duration: 0.3,
              ease: "easeInOut",
            }}
          >
            <OverlayContent>
              <div className={`${isMobile ? " h-full w-full justify-center p-[24px]" : "w-[38%] p-[48px]"}
                 flex flex-col items-center  gap-y-1 ${dark_bg2_style} border ${dark_bd_style}`}>
                <ExclamationTriangleIcon className="h-12 w-12 text-yellow-500" />
                <h2 className={`text-xl font-bold ${dark_font_style}`}>HDMI signal error detected.</h2>
                <div className="text-left text-sm text-slate-700 dark:text-slate-300">
                  <div className="space-y-4">
                    <div className="space-y-2 text-black dark:text-white">
                      <AntdCard className={`${dark_bg_style} ${dark_bd_style}`}>
                        <ul className="list-disc space-y-2 pl-4 text-left">
                          <li>{$at("A loose or faulty HDMI connection")}</li>
                          <li>{$at("Incompatible resolution or refresh rate settings")}</li>
                          <li>{$at("Issues with the source device's HDMI output")}</li>
                        </ul>
                      </AntdCard>
                    </div>
                    <div className={`flex  w-full justify-center ${isMobile ? "flex-col h-[40px]" : "flex-row"}`}>
                      <AntdButton
                        href={"https://wiki.luckfox.com/intro"}
                        icon={<TiaoZhuanSvg />}
                        iconPosition={"end"}
                        className={isMobile ? "w-full !h-[40px]" : "w-[49%]"}
                      >
                        {$at("Learn more")}
                      </AntdButton>
                    </div>
                  </div>
                </div>
              </div>
            </OverlayContent>
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

interface NoAutoplayPermissionsOverlayProps {
  readonly show: boolean;
  readonly onPlayClick: () => void;
}

export function NoAutoplayPermissionsOverlay({
                                               show,
                                               onPlayClick,
                                             }: NoAutoplayPermissionsOverlayProps) {
  const { $at } = useReactAt();
  return (
    <AnimatePresence>
      {show && (
        <motion.div
          className="absolute inset-0 z-10 h-full w-full"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{
            duration: 0.3,
            ease: "easeInOut",
          }}
        >
          <OverlayContent>
            <div className="space-y-4">
              <h2 className="text-2xl font-extrabold text-black dark:text-white">
                Autoplay permissions required
              </h2>

              <div className="space-y-2 text-center">
                <div>
                  <Button
                    size="MD"
                    theme="primary"
                    LeadingIcon={LuPlay}
                    text="Manually start stream"
                    onClick={onPlayClick}
                  />
                </div>

                <div className="text-xs text-slate-600 dark:text-[#ffffff]">
                  {$at("Please adjust browser settings to enable autoplay")}
                </div>
              </div>
            </div>
          </OverlayContent>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

interface PointerLockBarProps {
  readonly show: boolean;
}

export function PointerLockBar({ show }: PointerLockBarProps) {
  const { $at } = useReactAt();
  return (
    <AnimatePresence mode="wait">
      {show ? (
        <motion.div
          className="flex w-full items-center justify-between bg-transparent"
          initial={{ opacity: 0, zIndex: 0 }}
          animate={{ opacity: 1, zIndex: 20 }}
          exit={{ opacity: 0, zIndex: 0 }}
          transition={{ duration: 0.5, ease: "easeInOut", delay: 0.5 }}
        >
          <div>
            <Card className="rounded-b-none shadow-none outline-0!">
              <div
                className="flex items-center justify-between border border-slate-800/50 px-4 py-2 outline-0 backdrop-blur-xs dark:border-slate-300/20 dark:bg-slate-800">
                <div className="flex items-center space-x-2">
                  <BsMouseFill className="h-4 w-4 text-blue-700 dark:text-blue-500" />
                  <span className="text-sm text-black dark:text-white">
                    {$at("Click on the video to enable mouse control")}
                  </span>
                </div>
              </div>
            </Card>
          </div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}
