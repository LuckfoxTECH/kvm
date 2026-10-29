// Monotonic epochs shared by the USB state queries and the switches that mutate
// that state.
//
// In HTTP mode each JSON-RPC call is an independent fetch whose callback fires
// whenever that fetch resolves, so there is no ordering between a query and a
// switch. A query issued before a switch can therefore answer after it, and
// applying that answer reverts the switch. Two distinct damages follow:
//
//   - a stale emulation `false` closes the keyboard/mouse input gates even
//     though the gadget is up, which reads as "the device stopped accepting
//     input" while everything on the device is healthy;
//   - a stale device composition rewrites the cached config and reopens those
//     gates in the middle of a reconfiguration, so reports are sent at
//     interfaces that are about to disappear.
//
// A query records the epoch it was issued under and drops its answer once a
// switch has bumped that epoch, so only answers newer than the last explicit
// action can reach the stores. The two epochs are kept apart because emulation
// and device composition are independent switches: one must not invalidate an
// answer about the other.
let emulationEpoch = 0;
let deviceConfigEpoch = 0;

export function currentUsbEmulationEpoch(): number {
  return emulationEpoch;
}

export function bumpUsbEmulationEpoch(): number {
  return ++emulationEpoch;
}

export function currentUsbDeviceConfigEpoch(): number {
  return deviceConfigEpoch;
}

export function bumpUsbDeviceConfigEpoch(): number {
  return ++deviceConfigEpoch;
}
