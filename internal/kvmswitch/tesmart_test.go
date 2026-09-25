package kvmswitch

import (
	"bytes"
	"testing"
)

func TestTESmartProtocol(t *testing.T) {
	// Protocol encoder / decoder sanity check tests
	expectedSwitchCmd := []byte{0xAA, 0xBB, 0x03, 0x01, 0x02, 0xEE}
	generatedSwitchCmd := []byte{cmdHeader[0], cmdHeader[1], cmdHeader[2], cmdSwitch, byte(2), cmdFooter}

	if !bytes.Equal(expectedSwitchCmd, generatedSwitchCmd) {
		t.Errorf("Switch command mismatch: got %x, want %x", generatedSwitchCmd, expectedSwitchCmd)
	}

	expectedQueryCmd := []byte{0xAA, 0xBB, 0x03, 0x10, 0x00, 0xEE}
	generatedQueryCmd := []byte{cmdHeader[0], cmdHeader[1], cmdHeader[2], cmdQuery, 0x00, cmdFooter}

	if !bytes.Equal(expectedQueryCmd, generatedQueryCmd) {
		t.Errorf("Query command mismatch: got %x, want %x", generatedQueryCmd, expectedQueryCmd)
	}
}
