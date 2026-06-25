package usbgadget

import (
	"os"
	"testing"
)

func TestCloseHidFilesIsNilSafe(t *testing.T) {
	var nilU *UsbGadget
	nilU.CloseHidFiles() // should not panic

	u := &UsbGadget{}
	u.CloseHidFiles()

	if u.keyboardHidFile != nil {
		t.Fatal("keyboardHidFile should remain nil")
	}
	if u.absMouseHidFile != nil {
		t.Fatal("absMouseHidFile should remain nil")
	}
	if u.relMouseHidFile != nil {
		t.Fatal("relMouseHidFile should remain nil")
	}
}

func TestCloseHidFilesClosesFilesAndCancelsKeyboardState(t *testing.T) {
	keyboardFile := tempFile(t)
	absMouseFile := tempFile(t)
	relMouseFile := tempFile(t)

	cancelled := false
	u := &UsbGadget{
		keyboardHidFile: keyboardFile,
		absMouseHidFile: absMouseFile,
		relMouseHidFile: relMouseFile,
		keyboardStateCancel: func() {
			cancelled = true
		},
	}

	u.CloseHidFiles()

	if !cancelled {
		t.Fatal("keyboardStateCancel should be called")
	}
	if u.keyboardStateCancel != nil {
		t.Fatal("keyboardStateCancel should be cleared")
	}
	if u.keyboardHidFile != nil {
		t.Fatal("keyboardHidFile should be nil")
	}
	if u.absMouseHidFile != nil {
		t.Fatal("absMouseHidFile should be nil")
	}
	if u.relMouseHidFile != nil {
		t.Fatal("relMouseHidFile should be nil")
	}

	assertClosed(t, "keyboard", keyboardFile)
	assertClosed(t, "absolute mouse", absMouseFile)
	assertClosed(t, "relative mouse", relMouseFile)
}

func tempFile(t *testing.T) *os.File {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "hid-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	return file
}

func assertClosed(t *testing.T, name string, file *os.File) {
	t.Helper()

	if _, err := file.Write([]byte{0}); err == nil {
		t.Fatalf("%s file should be closed", name)
	}
}
