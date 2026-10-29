import type { VideoState } from "@/hooks/stores";

// Shape of the device's VideoInputState struct (video.go). Both the
// "videoInputState" event and the getVideoState RPC carry the same payload.
export interface VideoInputStatePayload {
  ready?: boolean;
  error?: "no_signal" | "no_lock" | "out_of_range";
  width?: number;
  height?: number;
  frame_per_second?: number;
}

// applyVideoInputState feeds one device payload into the video store. The store
// keeps the connection state and the source timings apart: the timings are the
// HDMI input and must be shown as unknown while nothing is connected, whereas
// hdmiState still tracks "connecting" so the UI can keep polling.
export function applyVideoInputState(
  payload: VideoInputStatePayload | undefined,
  setHdmiState: VideoState["setHdmiState"],
  setVideoInput: VideoState["setVideoInput"],
) {
  if (!payload) return;
  setHdmiState(payload as Parameters<VideoState["setHdmiState"]>[0]);
  setVideoInput({
    width: payload.width ?? 0,
    height: payload.height ?? 0,
    fps: payload.frame_per_second ?? 0,
  });
}
