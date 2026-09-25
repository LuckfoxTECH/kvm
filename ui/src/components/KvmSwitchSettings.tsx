import React, { useEffect, useState } from 'react';
import { Input, Switch, Button, Card, Tag, Alert } from 'antd';
import { KvmSwitchConfig } from './KvmSwitchSelector';

export const KvmSwitchSettings: React.FC = () => {
  const [config, setConfig] = useState<KvmSwitchConfig>({
    enabled: false,
    switch_ip: '',
    switch_port: 5000,
    total_ports: 4,
    port_names: {},
    hotkeys_enabled: true,
    poll_interval_sec: 5
  });

  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; active_port?: number; latency_ms?: number; error?: string } | null>(null);

  useEffect(() => {
    fetch('/api/kvm-switch/config')
      .then(res => res.json())
      .then(data => setConfig(data))
      .catch(e => console.error(e));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await fetch('/api/kvm-switch/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config)
      });
      window.dispatchEvent(new CustomEvent('kvm-config-changed'));
      alert("Settings saved successfully.");
    } catch (e) {
      alert("Failed to save settings.");
    } finally {
      setSaving(false);
    }
  };

  const handleTestConnection = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await fetch('/api/kvm-switch/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ switch_ip: config.switch_ip, switch_port: config.switch_port })
      });
      const data = await res.json();
      setTestResult(data);
    } catch (e: any) {
      setTestResult({ success: false, error: e.toString() });
    } finally {
      setTesting(false);
    }
  };

  const handlePortNameChange = (port: number, name: string) => {
    setConfig(prev => ({
      ...prev,
      port_names: { ...prev.port_names, [port.toString()]: name }
    }));
  };

  const [networkIP, setNetworkIP] = useState('');
  const [networkMask, setNetworkMask] = useState('');
  const [networkGateway, setNetworkGateway] = useState('');
  const [networkPort, setNetworkPort] = useState(5000);
  const [fetchingNetwork, setFetchingNetwork] = useState(false);
  const [applyingNetwork, setApplyingNetwork] = useState(false);

  const fetchNetwork = async () => {
    setFetchingNetwork(true);
    try {
      const res = await fetch('/api/kvm-switch/network');
      const data = await res.json();
      if (res.ok) {
        setNetworkIP(data.ip || '');
        setNetworkMask(data.mask || '');
        setNetworkGateway(data.gateway || '');
        setNetworkPort(data.port || 5000);
      } else {
        alert("Failed to get network config: " + (data.error || ""));
      }
    } catch (e) {
      alert("Failed to fetch network config.");
    } finally {
      setFetchingNetwork(false);
    }
  };

  const applyNetwork = async () => {
    if (!window.confirm("Warning: Applying network settings requires a hard reboot of the switch. Proceed?")) return;
    setApplyingNetwork(true);
    try {
      const res = await fetch('/api/kvm-switch/network', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ip: networkIP, mask: networkMask, gateway: networkGateway, port: networkPort })
      });
      if (res.ok) {
        alert("Network settings applied.");
      } else {
        const data = await res.json();
        alert("Failed to apply network settings: " + (data.error || ""));
      }
    } catch (e) {
      alert("Failed to apply network settings.");
    } finally {
      setApplyingNetwork(false);
    }
  };

  return (
    <>
    <Card title="TESmart KVM Switch Configuration" style={{ margin: '16px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
        <div>
          <Switch 
            checked={config.enabled} 
            onChange={(v: boolean) => setConfig({ ...config, enabled: v })}
          />
          <span style={{ marginLeft: '8px' }}>Enable TESmart Switch Integration</span>
        </div>

        <div style={{ display: 'flex', gap: '16px', alignItems: 'end' }}>
          <div style={{ flex: 1 }}>
            <label>Switch IP Address</label>
            <Input 
              value={config.switch_ip}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setConfig({ ...config, switch_ip: e.target.value })}
            />
          </div>
          <div style={{ width: '120px' }}>
            <label>Switch Port</label>
            <Input 
              type="number"
              value={config.switch_port}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setConfig({ ...config, switch_port: parseInt(e.target.value, 10) || 5000 })}
            />
          </div>
          <Button onClick={handleTestConnection} loading={testing}>
            Test Connection
          </Button>
        </div>

        {testResult && (
          <div>
            {testResult.success ? (
              <Tag color="success">
                Connected successfully! Active Port: {testResult.active_port} (Latency: {testResult.latency_ms}ms)
              </Tag>
            ) : (
              <Alert type="error" message={`Connection failed: ${testResult.error}`} showIcon />
            )}
          </div>
        )}

        <div>
          <label>Total Ports</label>
          <Input 
            type="number"
            value={config.total_ports}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setConfig({ ...config, total_ports: parseInt(e.target.value, 10) || 4 })}
          />
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <h4>Port Names</h4>
          {Array.from({ length: config.total_ports }, (_, i) => i + 1).map(port => (
            <div key={port}>
              <label>Port {port} Name</label>
              <Input
                value={config.port_names[port.toString()] || ''}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => handlePortNameChange(port, e.target.value)}
              />
            </div>
          ))}
        </div>

        <div>
          <Switch 
            checked={config.hotkeys_enabled} 
            onChange={(v: boolean) => setConfig({ ...config, hotkeys_enabled: v })}
          />
          <span style={{ marginLeft: '8px' }}>Enable Hotkeys (Ctrl + Alt + [1-8])</span>
        </div>

        <Button type="primary" onClick={handleSave} loading={saving}>
          Save Settings
        </Button>
      </div>
    </Card>
    
    <Card title="TESmart Switch Network Configuration" style={{ margin: '16px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
        <div style={{ display: 'flex', gap: '16px' }}>
          <div style={{ flex: 1 }}>
            <label>Switch IP</label>
            <Input 
              value={networkIP}
              onChange={(e) => setNetworkIP(e.target.value)}
            />
          </div>
          <div style={{ flex: 1 }}>
            <label>Subnet Mask</label>
            <Input 
              value={networkMask}
              onChange={(e) => setNetworkMask(e.target.value)}
            />
          </div>
        </div>
        <div style={{ display: 'flex', gap: '16px' }}>
          <div style={{ flex: 1 }}>
            <label>Switch Gateway</label>
            <Input 
              value={networkGateway}
              onChange={(e) => setNetworkGateway(e.target.value)}
            />
          </div>
          <div style={{ flex: 1 }}>
            <label>Port</label>
            <Input 
              type="number"
              value={networkPort}
              onChange={(e) => setNetworkPort(parseInt(e.target.value, 10) || 5000)}
            />
          </div>
        </div>
        <div style={{ display: 'flex', gap: '8px' }}>
          <Button onClick={fetchNetwork} loading={fetchingNetwork}>
            Refresh
          </Button>
          <Button type="primary" danger onClick={applyNetwork} loading={applyingNetwork}>
            Apply Network Settings
          </Button>
        </div>
      </div>
    </Card>
    </>
  );
};
