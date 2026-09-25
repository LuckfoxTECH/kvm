# TESmart KVM Switch Integration

PicoKVM includes native integration for TESmart Matrix / KVM Switches over TCP (Port 5000).

## Features
- **Auto-Polling:** The KVM web interface will periodically poll the switch to determine the active port.
- **Switching Ports:** Select a port using the dropdown menu in the PicoKVM UI, or use keyboard hotkeys (`Ctrl + Alt + [1-8]`).
- **Network Configuration:** Configure the Switch's IP Address, Subnet Mask, Gateway, and Port natively from the UI via ASCII TCP commands.

## Setup
1. Enter the Switch's IP Address and Port (default 5000) in the settings panel.
2. Ensure `Enable TESmart Switch Integration` is toggled ON.
3. Configure your Port Names for clarity.
4. Click `Save Settings`.

## Network Reset
If you accidentally lock yourself out of the switch by pushing a bad IP, refer to the TESmart user manual to perform a factory reset on the ethernet module, usually via RS232 or IR control.
