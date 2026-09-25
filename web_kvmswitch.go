package kvm

import (
	"net/http"

	"kvm/internal/kvmswitch"

	"github.com/gin-gonic/gin"
)

func handleGetKvmSwitchConfig(c *gin.Context) {
	configLock.Lock()
	defer configLock.Unlock()
	
	if config.KvmSwitch == nil {
		c.JSON(http.StatusOK, defaultConfig.KvmSwitch)
		return
	}
	c.JSON(http.StatusOK, config.KvmSwitch)
}

func handleSetKvmSwitchConfig(c *gin.Context) {
	var req KvmSwitchConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	configLock.Lock()
	if config.KvmSwitch == nil {
		config.KvmSwitch = &KvmSwitchConfig{}
	}
	*config.KvmSwitch = req
	configLock.Unlock()

	SaveConfig()

	if kvmswitch.GlobalSwitch != nil {
		kvmswitch.GlobalSwitch.UpdateConfig(req.Enabled, req.SwitchIP, req.SwitchPort, req.PollIntervalSec)
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func handleGetKvmSwitchStatus(c *gin.Context) {
	if kvmswitch.GlobalSwitch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "switch not initialized"})
		return
	}
	port := kvmswitch.GlobalSwitch.GetActivePort()
	c.JSON(http.StatusOK, gin.H{"active_port": port})
}

func handleSetKvmSwitchSelect(c *gin.Context) {
	var req struct {
		Port int `json:"port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if kvmswitch.GlobalSwitch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "switch not initialized"})
		return
	}

	err := kvmswitch.GlobalSwitch.SwitchPort(req.Port)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func handleTestKvmSwitchConnection(c *gin.Context) {
	var req struct {
		SwitchIP   string `json:"switch_ip"`
		SwitchPort int    `json:"switch_port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	activePort, latency, err := kvmswitch.TestConnection(req.SwitchIP, req.SwitchPort)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"active_port": activePort,
		"latency_ms": latency.Milliseconds(),
	})
}

func handleGetKvmSwitchNetwork(c *gin.Context) {
	if kvmswitch.GlobalSwitch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "switch not initialized"})
		return
	}
	ip, mask, gw, port, err := kvmswitch.GlobalSwitch.QueryNetworkInfo()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ip": ip, "mask": mask, "gateway": gw, "port": port})
}

func handleSetKvmSwitchNetwork(c *gin.Context) {
	var req struct {
		IP      string `json:"ip"`
		Mask    string `json:"mask"`
		Gateway string `json:"gateway"`
		Port    int    `json:"port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if kvmswitch.GlobalSwitch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "switch not initialized"})
		return
	}

	err := kvmswitch.GlobalSwitch.ConfigureNetwork(req.IP, req.Mask, req.Gateway, req.Port)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

