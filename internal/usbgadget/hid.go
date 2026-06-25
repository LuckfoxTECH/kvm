package usbgadget

import "time"

func (u *UsbGadget) CloseHidFiles() {
	if u == nil {
		return
	}
	u.keyboardLock.Lock()
	defer u.keyboardLock.Unlock()
	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()
	u.relMouseLock.Lock()
	defer u.relMouseLock.Unlock()

	u.closeKeyboardHidFileLocked()

	if u.absMouseHidFile != nil {
		u.absMouseHidFile.Close()
		u.absMouseHidFile = nil
	}

	if u.relMouseHidFile != nil {
		u.relMouseHidFile.Close()
		u.relMouseHidFile = nil
	}
}

func (u *UsbGadget) resetUserInputTime() {
	u.lastUserInput = time.Now()
}

func (u *UsbGadget) GetLastUserInputTime() time.Time {
	return u.lastUserInput
}
