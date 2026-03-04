package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

func (a *Agent) register(ctx context.Context) error {
	// Load existing device ID if present
	deviceID := a.config.Agent.DeviceID
	if deviceID == "" {
		if data, err := os.ReadFile(IDPath()); err == nil {
			deviceID = string(data)
		}
	}

	// Collect hardware info for registration
	sysCollector := &collectors.SystemCollector{}
	sysData, err := sysCollector.Collect(ctx)
	if err != nil {
		slog.Warn("failed to collect system info for registration", "error", err)
	}

	hostname := a.config.Agent.DeviceName
	if hostname == "" {
		hostname, _ = os.Hostname()
	}

	var hwProfile *models.HardwareProfile
	if sysInfo, ok := sysData.(*models.SystemInfo); ok && sysInfo != nil {
		hwProfile = &models.HardwareProfile{
			CPUModel:       sysInfo.CPUModel,
			CPUCores:       sysInfo.CPUCores,
			CPUThreads:     sysInfo.CPUThreads,
			TotalRAMMB:     sysInfo.TotalRAMMB,
			BoardModel:     sysInfo.BoardModel,
			SerialNumber:   sysInfo.SerialNumber,
			BIOSVersion:    sysInfo.BIOSVersion,
			BIOSDate:       sysInfo.BIOSDate,
			Virtualization: sysInfo.Virtualization,
		}
		if hostname == "" {
			hostname = sysInfo.Hostname
		}
	}

	reg := &models.DeviceRegistration{
		Hostname:        hostname,
		Arch:            sysCollector.GetArch(),
		AgentVersion:    a.version,
		Tags:            a.config.Agent.Tags,
		DeviceID:        deviceID,
		HardwareProfile: hwProfile,
	}

	resp, err := a.client.Register(ctx, reg)
	if err != nil {
		return err
	}

	// Persist device ID
	if resp.DeviceID != "" {
		a.config.Agent.DeviceID = resp.DeviceID
		idDir := filepath.Dir(IDPath())
		os.MkdirAll(idDir, 0755)
		os.WriteFile(IDPath(), []byte(resp.DeviceID), 0644)
	}

	// Store API key if new registration
	if resp.APIKey != "" {
		a.config.Server.APIKey = resp.APIKey
		a.client.apiKey = resp.APIKey
		a.config.Save(a.configPath)
	}

	slog.Info("registered with server",
		"device_id", resp.DeviceID,
		"short_id", resp.ShortID,
	)
	return nil
}
