import { forwardRef } from "react";
import { isMobile } from 'react-device-detect';

import { useHidStore } from "@/hooks/stores";
import { isKeyboardTapSuppressed } from "@/utils/keyboardTapGuard";

interface VideoElementProps {
  onPlaying: () => void;
  style: React.CSSProperties;
  className: string;
}

export const VideoElement = forwardRef<HTMLVideoElement, VideoElementProps>(
  ({ onPlaying, style, className }, ref) => {
    const setVirtualKeyboardEnabled = useHidStore(state => state.setVirtualKeyboardEnabled);
    const isVirtualKeyboardEnabled = useHidStore(state => state.isVirtualKeyboardEnabled);
    const allowTapToOpenVirtualKeyboard = useHidStore(state => state.allowTapToOpenVirtualKeyboard);

    const handleClick = () => {
      if (isKeyboardTapSuppressed()) return;
      if (isMobile && allowTapToOpenVirtualKeyboard && !isVirtualKeyboardEnabled) {
        setVirtualKeyboardEnabled(true);
      }
    };

    const mergedStyle = {
      ...style,
      maxWidth: '100vw',
      width: '100vw',
      height: 'auto',
    };

    return (
      <video
        ref={ref}
        autoPlay={true}
        controls={false}
        onPlaying={onPlaying}
        onPlay={onPlaying}
        onClick={handleClick}
        muted={true}
        playsInline
        disablePictureInPicture
        controlsList="nofullscreen"
        style={mergedStyle}
        className={className}
      />
    );
  }
);

VideoElement.displayName = "VideoElement";
