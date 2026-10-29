import { useState, useEffect } from "react";
import { Button as AntdButton , Slider , Checkbox, Select, Modal, InputNumber, Tabs, Typography } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";
import { isMobile } from "react-device-detect";

import { TextAreaWithLabel } from "@components/TextArea";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import { useSettingsStore } from "@/hooks/stores";
import { SettingsItem, SettingsItemNew } from "@components/Settings/SettingsView";

import notifications from "../../../notifications";

const { Text } = Typography;





// normalizeEdid lowercases, strips whitespace and an optional 0x prefix so two
// representations of the same EDID compare equal.
const normalizeEdid = (value: string) => value.trim().replace(/^0x/i, "").replace(/\s+/g, "").toLowerCase();

// builtinEdids are extra presets. "KVM Default" is not listed here: it comes from
// the backend (getDefaultEDID) so the UI and the device never disagree.
const builtinEdids = [
  {
    value:
      "00FFFFFFFFFFFF00047265058A3F6101101E0104A53420783FC125A8554EA0260D5054BFEF80714F8140818081C081008B009500B300283C80A070B023403020360006442100001A000000FD00304C575716010A202020202020000000FC0042323436574C0A202020202020000000FF0054384E4545303033383532320A01F802031CF14F90020304050607011112131415161F2309070783010000011D8018711C1620582C250006442100009E011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E9600064421000018C344806E70B028401720A80406442100001E00000000000000000000000000000000000000000000000000000096",
    label: "Acer B246WL, 1920x1200",
  },
  {
    value:
      "00FFFFFFFFFFFF0006B3872401010101021F010380342078EA6DB5A7564EA0250D5054BF6F00714F8180814081C0A9409500B300D1C0283C80A070B023403020360006442100001A000000FD00314B1E5F19000A202020202020000000FC00504132343851560A2020202020000000FF004D314C4D51533035323135370A014D02032AF14B900504030201111213141F230907078301000065030C001000681A00000101314BE6E2006A023A801871382D40582C450006442100001ECD5F80B072B0374088D0360006442100001C011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E960006442100001800000000000000000000000000DC",
    label: "ASUS PA248QV, 1920x1200",
  },
  {
    value:
      "00FFFFFFFFFFFF0010AC132045393639201E0103803C22782ACD25A3574B9F270D5054A54B00714F8180A9C0D1C00101010101010101023A801871382D40582C450056502100001E000000FF00335335475132330A2020202020000000FC0044454C4C204432373231480A20000000FD00384C1E5311000A202020202020018102031AB14F90050403020716010611121513141F65030C001000023A801871382D40582C450056502100001E011D8018711C1620582C250056502100009E011D007251D01E206E28550056502100001E8C0AD08A20E02D10103E960056502100001800000000000000000000000000000000000000000000000000000000004F",
    label: "DELL D2721H, 1920x1080",
  },
];

const streamQualityOptions = [
  { value: "1", label: "High" },
  { value: "0.5", label: "Medium" },
  { value: "0.1", label: "Low" },
];

type RcQpParams = {
  s32FirstFrameStartQp: number;
  u32StepQp: number;
  u32MinQp: number;
  u32MaxQp: number;
  u32MinIQp: number;
  u32MaxIQp: number;
  s32DeltIpQp: number;
  s32MaxReEncodeTimes: number;
  u32FrmMaxQp: number;
  u32FrmMinQp: number;
  u32FrmMinIQp: number;
  u32FrmMaxIQp: number;
  u32MotionStaticSwitchFrmQp: number;
};

type VideoRcConfig = {
  h264: RcQpParams;
  h265: RcQpParams;
};

type RcSliderValues = {
  stepQp: number;
  minQp: number;
  minIQp: number;
  deltIpQp: number;
};

type RcSliderState = {
  h264: RcSliderValues;
  h265: RcSliderValues;
};

const clamp = (value: number, min: number, max: number) =>
  Math.min(max, Math.max(min, value));

const sliderValueToNumber = (value: number | number[]) =>
  Array.isArray(value) ? value[0] : value;

const DEFAULT_RC_CODEC: RcQpParams = {
  s32FirstFrameStartQp: 0,
  u32StepQp: 48,
  u32MinQp: 48,
  u32MaxQp: 51,
  u32MinIQp: 48,
  u32MaxIQp: 51,
  s32DeltIpQp: 7,
  s32MaxReEncodeTimes: 2,
  u32FrmMaxQp: 51,
  u32FrmMinQp: 48,
  u32FrmMinIQp: 51,
  u32FrmMaxIQp: 48,
  u32MotionStaticSwitchFrmQp: 50,
};

const DEFAULT_VIDEO_RC_CONFIG: VideoRcConfig = {
  h264: { ...DEFAULT_RC_CODEC },
  h265: { ...DEFAULT_RC_CODEC },
};

const isObjectRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

const toVideoRcConfigOrDefault = (value: unknown): VideoRcConfig => {
  if (!isObjectRecord(value)) return DEFAULT_VIDEO_RC_CONFIG;
  if (!("h264" in value) || !("h265" in value)) return DEFAULT_VIDEO_RC_CONFIG;
  return value as VideoRcConfig;
};

const RC_LIMITS = {
  stepQp: { min: 1, max: 51 },
  maxQp: { min: 1, max: 51 },
  maxIQp: { min: 1, max: 51 },
  deltIpQp: { min: -7, max: 7 },
  maxReEncodeTimes: { min: 0, max: 3 },
  frmQp: { min: 1, max: 51 },
};

const normalizeRcCodec = (codec: RcQpParams): RcQpParams => {
  const maxQp = clamp(Number(codec.u32MaxQp), RC_LIMITS.maxQp.min, RC_LIMITS.maxQp.max);
  const maxIQp = clamp(Number(codec.u32MaxIQp), RC_LIMITS.maxIQp.min, RC_LIMITS.maxIQp.max);
  const minQp = clamp(Number(codec.u32MinQp), RC_LIMITS.maxQp.min, maxQp);
  const minIQp = clamp(Number(codec.u32MinIQp), RC_LIMITS.maxIQp.min, maxIQp);

  return {
    ...codec,
    s32FirstFrameStartQp: clamp(Number(codec.s32FirstFrameStartQp), RC_LIMITS.frmQp.min, RC_LIMITS.frmQp.max),
    u32StepQp: clamp(Number(codec.u32StepQp), RC_LIMITS.stepQp.min, RC_LIMITS.stepQp.max),
    u32MaxQp: maxQp,
    u32MinQp: minQp,
    u32MaxIQp: maxIQp,
    u32MinIQp: minIQp,
    s32DeltIpQp: clamp(Number(codec.s32DeltIpQp), RC_LIMITS.deltIpQp.min, RC_LIMITS.deltIpQp.max),
    s32MaxReEncodeTimes: clamp(
      Number(codec.s32MaxReEncodeTimes),
      RC_LIMITS.maxReEncodeTimes.min,
      RC_LIMITS.maxReEncodeTimes.max,
    ),
    u32FrmMaxQp: clamp(Number(codec.u32FrmMaxQp), RC_LIMITS.frmQp.min, RC_LIMITS.frmQp.max),
    u32FrmMinQp: clamp(Number(codec.u32FrmMinQp), RC_LIMITS.frmQp.min, RC_LIMITS.frmQp.max),
    u32FrmMinIQp: clamp(Number(codec.u32FrmMinIQp), RC_LIMITS.frmQp.min, RC_LIMITS.frmQp.max),
    u32FrmMaxIQp: clamp(Number(codec.u32FrmMaxIQp), RC_LIMITS.frmQp.min, RC_LIMITS.frmQp.max),
    u32MotionStaticSwitchFrmQp: clamp(
      Number(codec.u32MotionStaticSwitchFrmQp),
      RC_LIMITS.frmQp.min,
      RC_LIMITS.frmQp.max,
    ),
  };
};

const normalizeVideoRcConfig = (config: VideoRcConfig): VideoRcConfig => ({
  h264: normalizeRcCodec(config.h264),
  h265: normalizeRcCodec(config.h265),
});

const sliderValuesFromCodec = (codec: RcQpParams): RcSliderValues => ({
  stepQp: clamp(Number(codec.u32StepQp), 1, 50),
  minQp: clamp(Number(codec.u32MinQp), 1, 50),
  minIQp: clamp(Number(codec.u32MinIQp), 1, 50),
  deltIpQp: clamp(Number(codec.s32DeltIpQp), -7, 7),
});

const sliderStateFromConfig = (config: VideoRcConfig): RcSliderState => ({
  h264: sliderValuesFromCodec(config.h264),
  h265: sliderValuesFromCodec(config.h265),
});

export default function SettingsVideoSide() {
  const { $at } = useReactAt();
  const [send] = useJsonRpc();
  const [npuAppStatus, setNpuAppStatus] = useState(false);
  const [streamQuality, setStreamQuality] = useState("1");
  const [streamEncodecType, setStreamEncodecType] = useState("avc");
  const [customEdidValue, setCustomEdidValue] = useState<string | null>(null);
  const [edid, setEdid] = useState<string | null>(null);
  const [defaultEdid, setDefaultEdid] = useState<string | null>(null);
  const [forceHpd, setForceHpd] = useState(false);
  const [videoRcConfig, setVideoRcConfig] = useState<VideoRcConfig>(DEFAULT_VIDEO_RC_CONFIG);
  const [rcSliderValues, setRcSliderValues] = useState<RcSliderState>(
    sliderStateFromConfig(DEFAULT_VIDEO_RC_CONFIG),
  );
  const [showRcAdvanced, setShowRcAdvanced] = useState(false);
  const [rcDraftConfig, setRcDraftConfig] = useState<VideoRcConfig | null>(null);

  // Video enhancement settings from store
  const videoSaturation = useSettingsStore(state => state.videoSaturation);
  const setVideoSaturation = useSettingsStore(state => state.setVideoSaturation);
  const videoBrightness = useSettingsStore(state => state.videoBrightness);
  const setVideoBrightness = useSettingsStore(state => state.setVideoBrightness);
  const videoContrast = useSettingsStore(state => state.videoContrast);
  const setVideoContrast = useSettingsStore(state => state.setVideoContrast);

  const currentCodec: "h264" | "h265" = streamEncodecType === "hevc" ? "h265" : "h264";
  const currentSliders = rcSliderValues[currentCodec];

  const applySliderToCodec = (codec: RcQpParams, sliders: RcSliderValues): RcQpParams => ({
      ...codec,
      u32StepQp: sliders.stepQp,
      u32MinQp: sliders.minQp,
      u32MinIQp: sliders.minIQp,
      s32DeltIpQp: sliders.deltIpQp,
    });

  const applyRcBasicConfig = () => {
    const nextRcConfig = normalizeVideoRcConfig({
      ...videoRcConfig,
      [currentCodec]: applySliderToCodec(videoRcConfig[currentCodec], currentSliders),
    });
    send("setVideoRc", { params: nextRcConfig }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to set video RC: ${resp.error.data || "Unknown error"}`);
        return;
      }
      setVideoRcConfig(nextRcConfig);
      setRcDraftConfig(nextRcConfig);
      setRcSliderValues(sliderStateFromConfig(nextRcConfig));
      notifications.success("Video RC updated");
    });
  };

  const updateRcDraftField = (
    codec: "h264" | "h265",
    field: keyof RcQpParams,
    value: number,
  ) => {
    setRcDraftConfig(prev => {
      if (!prev) return prev;
      const nextCodec = { ...prev[codec], [field]: value };
      const normalizedCodec = normalizeRcCodec(nextCodec);
      return {
        ...prev,
        [codec]: normalizedCodec,
      };
    });
  };

  const openRcAdvancedModal = () => {
    const nextDraft = normalizeVideoRcConfig({
      ...videoRcConfig,
      [currentCodec]: applySliderToCodec(videoRcConfig[currentCodec], currentSliders),
    });
    setRcDraftConfig(nextDraft);
    setShowRcAdvanced(true);
  };

  const applyRcAdvancedConfig = () => {
    if (!rcDraftConfig) {
      return;
    }
    const normalized = normalizeVideoRcConfig(rcDraftConfig);
    send("setVideoRc", { params: normalized }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to set video RC: ${resp.error.data || "Unknown error"}`);
        return;
      }
      setVideoRcConfig(normalized);
      setRcDraftConfig(normalized);
      setRcSliderValues(sliderStateFromConfig(normalized));
      notifications.success("Video RC updated");
      setShowRcAdvanced(false);
    });
  };

  const renderRcAdvancedForm = (codec: "h264" | "h265") => {
    const current = rcDraftConfig?.[codec];
    if (!current) return null;

    const fields: Array<{
      key: keyof RcQpParams;
      label: string;
      min?: number;
      max?: number;
    }> = [
      { key: "s32FirstFrameStartQp", label: "FirstFrameStartQp", min: 1, max: 51 },
      { key: "u32StepQp", label: "StepQp", min: 1, max: 51 },
      { key: "u32MaxQp", label: "MaxQp", min: 1, max: 51 },
      { key: "u32MinQp", label: "MinQp", min: 1, max: Number(current.u32MaxQp) || 51 },
      { key: "u32MaxIQp", label: "MaxIQp", min: 1, max: 51 },
      { key: "u32MinIQp", label: "MinIQp", min: 1, max: Number(current.u32MaxIQp) || 51 },
      { key: "s32DeltIpQp", label: "DeltIpQp", min: -7, max: 7 },
      { key: "s32MaxReEncodeTimes", label: "MaxReEncodeTimes", min: 0, max: 3 },
      { key: "u32FrmMaxQp", label: "FrmMaxQp", min: 1, max: 51 },
      { key: "u32FrmMinQp", label: "FrmMinQp", min: 1, max: 51 },
      { key: "u32FrmMaxIQp", label: "FrmMaxIQp", min: 1, max: 51 },
      { key: "u32FrmMinIQp", label: "FrmMinIQp", min: 1, max: 51 },
      { key: "u32MotionStaticSwitchFrmQp", label: "MotionStaticSwitchFrmQp", min: 1, max: 51 },
    ];

    return (
      <div className="grid grid-cols-1 gap-3">
        {fields.map(field => (
          <div key={`${codec}-${field.key}`} className="flex items-center justify-between gap-3">
            <Text className="text-xs">{field.label}</Text>
            <InputNumber
              size="small"
              value={current[field.key]}
              min={field.min}
              max={field.max}
              step={1}
              style={{ width: 140 }}
              onChange={val => {
                if (typeof val !== "number") return;
                const safeValue =
                  field.min !== undefined && field.max !== undefined
                    ? clamp(Number(val), field.min, field.max)
                    : Number(val);
                updateRcDraftField(codec, field.key, safeValue);
              }}
            />
          </div>
        ))}
      </div>
    );
  };

  useEffect(() => {
    send("getNpuAppStatus", {}, resp => {
      if ("error" in resp) return;
      setNpuAppStatus(resp.result as boolean);
    });

    send("getStreamEncodecType", {}, resp => {
      if ("error" in resp) return;
      setStreamEncodecType(resp.result as string);
    });

    send("getStreamQualityFactor", {}, resp => {
      if ("error" in resp) return;
      setStreamQuality(String(resp.result));
    });

    send("getVideoRc", {}, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to get video RC: ${resp.error.data || "Unknown error"}`);
        const fallbackRc = normalizeVideoRcConfig(DEFAULT_VIDEO_RC_CONFIG);
        setVideoRcConfig(fallbackRc);
        setRcSliderValues(sliderStateFromConfig(fallbackRc));
        return;
      }

      const rc = normalizeVideoRcConfig(toVideoRcConfigOrDefault(resp.result));
      setVideoRcConfig(rc);
      setRcSliderValues(sliderStateFromConfig(rc));
    });

    // The backend owns the default EDID, so fetch it before comparing values.
    send("getDefaultEDID", {}, defaultResp => {
      const backendDefault =
        "error" in defaultResp || typeof defaultResp.result !== "string"
          ? null
          : (defaultResp.result as string);
      setDefaultEdid(backendDefault);

      const knownEdids = [
        ...(backendDefault ? [{ value: backendDefault, label: "KVM Default" }] : []),
        ...builtinEdids,
      ];

      send("getEDID", {}, resp => {
        if ("error" in resp) {
          notifications.error(`Failed to get EDID: ${resp.error.data || "Unknown error"}`);
          return;
        }

        const receivedEdid = resp.result as string;
        const receivedNormalized =
          typeof receivedEdid === "string" ? normalizeEdid(receivedEdid) : "";

        const matchingEdid = knownEdids.find(
          x => normalizeEdid(x.value) === receivedNormalized,
        );

        if (!matchingEdid) {
          console.debug("[EDID] device value does not match a known preset", {
            receivedLen: typeof receivedEdid === "string" ? receivedEdid.length : -1,
            defaultLen: backendDefault ? backendDefault.length : -1,
          });
        }

        if (matchingEdid) {
          // EDID is stored in uppercase in the UI
          setEdid(matchingEdid.value.toUpperCase());
          // Reset custom EDID value
          setCustomEdidValue(null);
        } else {
          setEdid("custom");
          setCustomEdidValue(receivedEdid);
        }
      });
    });

    send("getForceHpd", {}, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to get force EDID output: ${resp.error.data || "Unknown error"}`);
        setForceHpd(false);
        return;
      }

      setForceHpd(resp.result as boolean);
    });
  }, [send]);

  const handleForceHpdChange = (checked: boolean) => {
    send("setForceHpd", { forceHpd: checked }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to set force EDID output: ${resp.error.data || "Unknown error"}`);
        setForceHpd(!checked);
        return;
      }

      notifications.success(`Force EDID output ${checked ? "enabled" : "disabled"}`);
      setForceHpd(checked);
    });
  };

  const handleStreamEncodecTypeChange = (encodecType: string) => {
    send("setStreamEncodecType", { encodecType }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set stream encodec type: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }

      notifications.success(`Stream encodec type set to ${encodecType}`);
      setStreamEncodecType(encodecType);
      window.location.reload();
    });
  };

  const handleStreamQualityChange = (factor: string) => {
    send("setStreamQualityFactor", { factor: Number(factor) }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set stream quality: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }

      notifications.success(`Stream quality set to ${streamQualityOptions.find(x => x.value === factor)?.label}`);
      setStreamQuality(factor);
    });
  };

  const handleNpuAppStatusChange = (checked: boolean) => {
    send("setNpuAppStatus", { enable: checked }, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to set NPU app status: ${resp.error.data || "Unknown error"}`,
        );
        setNpuAppStatus(!checked);
        return;
      }
      
      notifications.success(`NPU app status set to ${checked ? "enabled" : "disabled"}`);
      setNpuAppStatus(checked);
    });
  };

  // Select options: the backend default first, then the built-in presets.
  const edidOptions = [
    ...(defaultEdid ? [{ value: defaultEdid.toUpperCase(), label: "KVM Default" }] : []),
    ...builtinEdids,
  ];

  const handleEDIDChange = (newEdid: string, label?: string) => {
    send("setEDID", { edid: newEdid }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to set EDID: ${resp.error.data || "Unknown error"}`);
        return;
      }

      notifications.success(
        `EDID set successfully to ${label ?? edidOptions.find(x => normalizeEdid(x.value) === normalizeEdid(newEdid))?.label ?? "custom EDID"}`,
      );
      // Update the EDID value in the UI
      setEdid(newEdid);
    });
  };

  // Restore to the backend default (empty string tells the backend to use it).
  const handleRestoreDefaultEDID = () => {
    send("setEDID", { edid: "" }, resp => {
      if ("error" in resp) {
        notifications.error(`Failed to restore default EDID: ${resp.error.data || "Unknown error"}`);
        return;
      }

      notifications.success("EDID restored to default");
      setCustomEdidValue(null);
      setEdid(defaultEdid ? defaultEdid.toUpperCase() : null);
    });
  };

  return (
    <div className="space-y-3 "
         onKeyUp={e => e.stopPropagation()}
         onKeyDown={e => e.stopPropagation()}
    >
      <div className="space-y-4">
        <SettingsItem
          title={$at("Encodec Type")}
          description={""}
        >
          <Select
            className={isMobile ? "w-full bg-transparent" : ""}
            value={streamEncodecType}
            options={[
              { value: "avc", label: "H.264 (AVC)" },
              { value: "hevc", label: "H.265 (HEVC)" },
            ]}
            onChange={e => handleStreamEncodecTypeChange(e)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("Stream Quality")}
          description={""}
        >
          <Select
            className={isMobile ? "w-full bg-transparent" : ""}
            value={streamQuality}
            options={streamQualityOptions}
            onChange={e => handleStreamQualityChange(e)}
          />
        </SettingsItem>

        <SettingsItem
          title={$at("RC Control")}
          description={$at("Adjust rate control QP settings for better balance between quality and bitrate")}
        />
        <div className="space-y-4">
          <SettingsItemNew
            title={$at("StepQp")}
            description={String(currentSliders.stepQp)}
            className={"flex-col w-full h-[40px]"}
          >
            <Slider
              min={1}
              max={51}
              step={1}
              value={currentSliders.stepQp}
              onChange={value => {
                const nextStepQp = clamp(sliderValueToNumber(value), 1, 50);
                setRcSliderValues(prev => ({
                  ...prev,
                  [currentCodec]: {
                    ...prev[currentCodec],
                    stepQp: nextStepQp,
                  },
                }));
              }}
              className={"w-full"}
            />
          </SettingsItemNew>

          <SettingsItemNew
            title={$at("MinQp")}
            description={String(currentSliders.minQp)}
            className={"flex-col w-full h-[40px]"}
          >
            <Slider
              min={1}
              max={50}
              step={1}
              value={currentSliders.minQp}
              onChange={value => {
                const nextMinQp = clamp(sliderValueToNumber(value), 1, 50);
                setRcSliderValues(prev => ({
                  ...prev,
                  [currentCodec]: {
                    ...prev[currentCodec],
                    minQp: nextMinQp,
                  },
                }));
              }}
              className={"w-full"}
            />
          </SettingsItemNew>

          <SettingsItemNew
            title={$at("MinIQp")}
            description={String(currentSliders.minIQp)}
            className={"flex-col w-full h-[40px]"}
          >
            <Slider
              min={1}
              max={50}
              step={1}
              value={currentSliders.minIQp}
              onChange={value => {
                const nextMinIQp = clamp(sliderValueToNumber(value), 1, 50);
                setRcSliderValues(prev => ({
                  ...prev,
                  [currentCodec]: {
                    ...prev[currentCodec],
                    minIQp: nextMinIQp,
                  },
                }));
              }}
              className={"w-full"}
            />
          </SettingsItemNew>

          <SettingsItemNew
            title={$at("DetlpQp")}
            description={String(currentSliders.deltIpQp)}
            className={"flex-col w-full h-[40px]"}
          >
            <Slider
              min={-7}
              max={7}
              step={1}
              value={currentSliders.deltIpQp}
              onChange={value => {
                const nextDeltIpQp = clamp(sliderValueToNumber(value), -7, 7);
                setRcSliderValues(prev => ({
                  ...prev,
                  [currentCodec]: {
                    ...prev[currentCodec],
                    deltIpQp: nextDeltIpQp,
                  },
                }));
              }}
              className={"w-full"}
            />
          </SettingsItemNew>

          <div className="flex justify-end gap-2">
            <AntdButton onClick={openRcAdvancedModal}>
              {$at("Advanced")}
            </AntdButton>
            <AntdButton type="primary" onClick={applyRcBasicConfig}>
              {$at("Apply")}
            </AntdButton>
          </div>
        </div>

        <SettingsItem
          title={$at("NPU Application")}
          badge="Experimental"
          description={$at("Enable NPU to Object Detection")}
          noCol
          className="flex-row items-center"
        >
          <Checkbox
            checked={npuAppStatus}
            onChange={e => handleNpuAppStatusChange(e.target.checked)}
          />
        </SettingsItem>

        {/* Video Enhancement Settings */}
        <SettingsItem
          title={$at("Video Enhancement")}
          description={$at("Adjust color settings to make the video output more vibrant and colorful")}
        />

        <div className="space-y-4">
          <SettingsItemNew
            title={$at("Saturation")}
            description={`${videoSaturation.toFixed(1)}x`}
            className={"flex-col w-full h-[40px]"}
          >

            <Slider
              min={0.5}
              max={2.0}
              step={0.1}
              value={videoSaturation}
              onChange={value => setVideoSaturation(value)}
              className={"w-full"}
              styles={{
                rail: {
                  borderRadius: '4px'
                },
                track: {
                  borderRadius: '4px'
                }
              }}
            >
            </Slider>
          </SettingsItemNew>

          <SettingsItemNew
            title={$at("Brightness")}
            description={`${videoBrightness.toFixed(1)}x`}
            className={"flex-col w-full h-[40px]"}
          >
            <Slider
              min={0.5}
              max={2.0}
              step={0.1}
              value={videoBrightness}
              onChange={value => setVideoBrightness(value)}
              className={"w-full"}
              styles={{
                rail: {
                  borderRadius: '4px'
                },
                track: {
                  borderRadius: '4px'
                }
              }}
            >
            </Slider>
          </SettingsItemNew>

          <SettingsItemNew
            title={$at("Contrast")}
            description={`${videoContrast.toFixed(1)}x`}
            className={"flex-col w-full h-[40px]"}
          >
            <Slider
              min={0.5}
              max={2.0}
              step={0.1}
              value={videoContrast}
              onChange={value => setVideoContrast(value)}
              className={"w-full"}
              styles={{
                rail: {
                  borderRadius: '4px'
                },
                track: {
                  borderRadius: '4px'
                }
              }}
            >
            </Slider>
          </SettingsItemNew>

          <div className="flex gap-2">
            <AntdButton
              className={"w-full my-2"}
              type={"primary"}
              onClick={() => {
                setVideoSaturation(1.0);
                setVideoBrightness(1.0);
                setVideoContrast(1.0);
              }}
            >{$at("Reset to Default")}</AntdButton>
          </div>
        </div>

        {/* EDID Force Output Setting */}
        <div className="w-full animate-fadeIn opacity-0" style={{ animationDuration: "0.7s", animationDelay: "0.1s" }}>
          <SettingsItem
            title={$at("Force EDID Output")}
            description={$at("Force EDID output even when no display is connected")}
            noCol
            className="flex-row items-center"
          >
            <Checkbox
              checked={forceHpd}
              onChange={e => handleForceHpdChange(e.target.checked)}
            />
          </SettingsItem>
        </div>

        <SettingsItem
          title="EDID"
          description={$at("Adjust the EDID settings for the display")}
        >
          <Select
            className={isMobile ? "w-full bg-transparent" : ""}

            defaultValue={customEdidValue !== null ? "custom" : edid ?? undefined}
            value={customEdidValue !== null ? "custom" : edid ?? undefined}
            optionLabelProp={"label"}
            onChange={e => {
              if (e === "custom") {
                setEdid("custom");
                setCustomEdidValue("");
              } else {
                setCustomEdidValue(null);
                handleEDIDChange(e);
              }
            }}
            options={[...edidOptions, { value: "custom", label: "Custom" }]}
          />
        </SettingsItem>

        {customEdidValue !== null && (
          <>
            <SettingsItem
              title={$at("Custom EDID")}
              description={$at("EDID details video mode compatibility. Default settings works in most cases, but unique UEFI/BIOS might need adjustments.")}
            />
            <TextAreaWithLabel
              label={$at("EDID File")}
              placeholder="00F..."
              rows={3}
              value={customEdidValue}
              onChange={e => setCustomEdidValue(e.target.value)}
            />
            <div className="flex justify-start gap-x-2">
              <AntdButton
                type="primary"
                onClick={() => handleEDIDChange(customEdidValue)}
              >{$at("Set Custom EDID")}</AntdButton>
              <AntdButton
                className={"border-2"}
                style={{
                  background: "transparent",
                  borderColor: "rgba(28,168,0,1)",
                  whiteSpace: "nowrap",
                }}
                onClick={() => {
                  handleRestoreDefaultEDID();
                }}

              ><div  className={"text-[rgba(28,168,0,1)]"}>{$at("Restore to default")}</div></AntdButton>
            </div>
          </>
        )}
      </div>
      <div className={"h-[10vh]"}></div>

      <Modal
        title={
          <Text strong style={{ fontSize: "16px" }}>
            {$at("RC Advanced Config")}
          </Text>
        }
        open={showRcAdvanced}
        onCancel={() => setShowRcAdvanced(false)}
        onOk={applyRcAdvancedConfig}
        okText={$at("Apply")}
        cancelText={$at("Cancel")}
        maskClosable={true}
        keyboard={true}
        width={520}
        styles={{
          body: {
            padding: "20px 24px",
          },
          header: {
            borderBottom: "1px solid #f0f0f0",
            padding: "16px 24px",
            marginBottom: 0,
          }
        }}
      >
        <Tabs
          items={[
            { key: "h264", label: "H264", children: renderRcAdvancedForm("h264") },
            { key: "h265", label: "H265", children: renderRcAdvancedForm("h265") },
          ]}
        />
      </Modal>
    </div>
  );
}
