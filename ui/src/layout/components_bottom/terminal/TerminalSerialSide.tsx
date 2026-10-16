import React, { useCallback, useEffect, useState } from "react";
import { Form, Select, Button } from "antd";
import { useReactAt } from "i18n-auto-extractor/react";

import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { useUiStore, useSerialStore } from "@/hooks/stores";
import { dark_bg2_style } from "@/layout/theme_color";

const { Option } = Select;
const FORM_ITEM_STYLE = { marginBottom: "1px" };
const CONTAINER_STYLE: React.CSSProperties = {
  padding: "20px",
  height: "100%",
  overflowY: "auto",
  maxWidth: "400px",
  margin: "0 auto",
};
const BUTTON_STYLE = { width: "100%" };

interface SerialSettings {
  baudRate: string;
  dataBits: string;
  stopBits: string;
  parity: string;
  connected?: boolean;
}

interface TerminalSerialSideProps {
  clearTerminal?: () => void;
}

const TerminalSerialSide: React.FC<TerminalSerialSideProps> = ({ clearTerminal }) => {
  const { $at } = useReactAt();
  const setDisableKeyboardFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const { isConnected, setIsConnected } = useSerialStore();
  const [form] = Form.useForm();
  const [send] = useJsonRpc();
  const [settings, setSettings] = useState<SerialSettings>({
    baudRate: "1200",
    dataBits: "8",
    stopBits: "1",
    parity: "none",
  });
  const [refreshFlag, setRefreshFlag] = useState(0);

  const defaultBaudRates = [
    "1200", "2400", "4800", "9600", "19200", "38400", "57600", "115200", "1500000"
  ];
  const [baudOptions, setBaudOptions] = useState(defaultBaudRates);

  const updateBaudOptions = (targetValue: string) => {
    if (defaultBaudRates.includes(targetValue)) {
      setBaudOptions(defaultBaudRates);
    } else {
      setBaudOptions([...defaultBaudRates, targetValue]);
    }
  };

  useEffect(() => {
    send("getSerialSettings", {}, resp => {
      if ("error" in resp) {
        notifications.error(
          `Failed to get serial settings: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      const newSettings = resp.result as SerialSettings;
      setSettings(newSettings);
      form.setFieldsValue(newSettings);

      // The port state lives in the backend: a page reload keeps the port open,
      // so trust the server instead of the local button state.
      setIsConnected(newSettings.connected === true);

      // Ensure current baud rate is in options
      if (!defaultBaudRates.includes(newSettings.baudRate)) {
        setBaudOptions([...defaultBaudRates, newSettings.baudRate]);
      } else {
        setBaudOptions(defaultBaudRates);
      }
    });
  }, [send, refreshFlag]);

  const reloadSettings = useCallback(() => {
    setRefreshFlag(prev => prev + 1);
  }, []);

  const handleSubmit = (values: SerialSettings) => {
    setSettings(values);
    console.log("Serial settings updated:", values);
  };

  const handleValuesChange = (changedValues: Partial<SerialSettings>) => {
    setSettings(prevSettings => ({
      ...prevSettings,
      ...changedValues
    }));
  };

  const submitSettingChange = useCallback(() => {
    form
      .validateFields()
      .then(values => {
        if (isConnected) {
          // If connected, disconnect - use disconnectSerial RPC
          send("disconnectSerial", {}, () => {
            // After disconnecting serial, reset IO input status
            send("resetIOInput", {}, () => {
              setIsConnected(false);
              notifications.success($at("Disconnected"));
              setRefreshFlag(f => f + 1);
            });
          });
        } else {
          // If disconnected, connect - first save settings, then connect
          console.log("Saving serial settings:", values);
          send(
            "setSerialSettings",
            {
              settings: {
                baudRate: values.baudRate,
                dataBits: values.dataBits,
                stopBits: values.stopBits,
                parity: values.parity,
              }
            },
            (resp) => {
              if ("error" in resp) {
                console.error("Failed to save serial settings:", resp.error);
                notifications.error($at("Failed to save settings"));
                return;
              }
              console.log("Serial settings saved successfully");
              // Now connect with the saved settings
              send("connectSerial", {}, () => {
                setIsConnected(true);
                notifications.success($at("Connected"));
                setRefreshFlag(f => f + 1);
              });
            }
          );
        }
      })
      .catch(() => {
        notifications.error($at("Invalid settings"));
      });
  }, [form, send, $at, isConnected, setIsConnected]);
  const handleSelectMouseDown = (e: { stopPropagation: () => void; }) => {
    e.stopPropagation();
    console.log('Select mouse down, but container wont know');
  };
  return (
    <div style={CONTAINER_STYLE} className={dark_bg2_style}>
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        onValuesChange={handleValuesChange}
        initialValues={settings}
        className="serial-config-form"
      >
        <Form.Item
          label="Baud Rate"
          name="baudRate"
          style={FORM_ITEM_STYLE}
          rules={[{ required: true, message: $at("Choose Baud Rate") }]}
        >
          <Select
            placeholder={$at("Choose Baud Rate")}
            onMouseDown={handleSelectMouseDown}
            onOpenChange={(open) => setDisableKeyboardFocusTrap(open)}
            showSearch
            disabled={isConnected}
            onChange={(value) => {
               setSettings(prev => ({...prev, baudRate: value}));
               updateBaudOptions(value);
            }}
            onSearch={(value) => {
              if (value && !baudOptions.includes(value)) {
                 setBaudOptions([...defaultBaudRates, value]);
              }
            }}
            onBlur={() => {
              updateBaudOptions(settings.baudRate);
            }}
          >
            {baudOptions.map(rate => (
              <Option key={rate} value={rate}>{rate}</Option>
            ))}
          </Select>
        </Form.Item>

        <Form.Item
          label="Data Bits"
          name="dataBits"
          style={FORM_ITEM_STYLE}
          rules={[{ required: true, message: $at("Choose Data Bits") }]}
        >
          <Select
            placeholder={$at("Choose Data Bits")}
            onMouseDown={handleSelectMouseDown}
            onOpenChange={(open) => setDisableKeyboardFocusTrap(open)}
            disabled={isConnected}
          >
            <Option value="5">5</Option>
            <Option value="6">6</Option>
            <Option value="7">7</Option>
            <Option value="8">8</Option>
          </Select>
        </Form.Item>

        <Form.Item
          label="Stop Bits"
          name="stopBits"
          style={FORM_ITEM_STYLE}
          rules={[{ required: true, message: $at("Choose Stop Bits") }]}
        >
          <Select
            placeholder={$at("Choose Stop Bits")}
            onMouseDown={handleSelectMouseDown}
            onOpenChange={(open) => setDisableKeyboardFocusTrap(open)}
            disabled={isConnected}
          >
            <Option value="1">1</Option>
            <Option value="1.5">1.5</Option>
            <Option value="2">2</Option>
          </Select>
        </Form.Item>

        <Form.Item
          label="Parity"
          name="parity"
          style={{ marginBottom: 20 }}
          rules={[{ required: true, message: $at("Choose Parity") }]}
        >
          <Select
            onMouseDown={handleSelectMouseDown}
            onOpenChange={(open) => setDisableKeyboardFocusTrap(open)}
            placeholder={$at("Choose Parity")}
            disabled={isConnected}
          >
            <Option value="none">None</Option>
            <Option value="even">Even</Option>
            <Option value="odd">Odd</Option>
            <Option value="mark">Mark</Option>
            <Option value="space">Space</Option>
          </Select>
        </Form.Item>

        <Form.Item style={FORM_ITEM_STYLE}>
          <Button type="primary" htmlType="submit" style={BUTTON_STYLE} onClick={submitSettingChange}>
            {isConnected ? $at("Disconnect") : $at("Connect")}
          </Button>
        </Form.Item>

        {clearTerminal && (
          <Form.Item style={{...FORM_ITEM_STYLE, marginTop: 10}}>
            <Button style={BUTTON_STYLE} onClick={clearTerminal} danger>
              {$at("Clear Terminal")}
            </Button>
          </Form.Item>
        )}
      </Form>
    </div>
  );
};

export default TerminalSerialSide;