package handlers

import (
	"net/http"
)

type securityOverview struct {
	TotalDevices      int `json:"total_devices"`
	DevicesReporting  int `json:"devices_reporting"`
	TotalFailedLogins int `json:"total_failed_logins"`
	TotalLoggedIn     int `json:"total_logged_in"`
	FirewallActive    int `json:"firewall_active"`
	FirewallInactive  int `json:"firewall_inactive"`
	SELinuxEnforcing  int `json:"selinux_enforcing"`
	AppArmorEnabled   int `json:"apparmor_enabled"`
}

type deviceSecurityInfo struct {
	DeviceID        string `json:"device_id"`
	Hostname        string `json:"hostname"`
	Status          string `json:"status"`
	SELinux         string `json:"selinux"`
	AppArmor        string `json:"apparmor"`
	FirewallStatus  string `json:"firewall_status"`
	FailedLogins24h int    `json:"failed_logins_24h"`
	LoggedInUsers   int    `json:"logged_in_users"`
	OpenPorts       []int  `json:"open_ports"`
}

// SecurityOverview handles GET /api/v1/security/overview.
func (h *Handlers) SecurityOverview(w http.ResponseWriter, r *http.Request) {
	snapshots, err := h.telemetry.GetAllLatestSnapshots(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get telemetry"}`, http.StatusInternalServerError)
		return
	}

	devices, err := h.devices.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list devices"}`, http.StatusInternalServerError)
		return
	}

	overview := securityOverview{TotalDevices: len(devices)}

	for _, snap := range snapshots {
		sec := snap.Data.Security
		if sec == nil {
			continue
		}
		overview.DevicesReporting++
		overview.TotalFailedLogins += sec.FailedLogins24h
		overview.TotalLoggedIn += sec.LoggedInUsers
		if sec.FirewallStatus == "active" || sec.FirewallStatus == "enabled" {
			overview.FirewallActive++
		} else if sec.FirewallStatus != "" {
			overview.FirewallInactive++
		}
		if sec.SELinux == "enforcing" {
			overview.SELinuxEnforcing++
		}
		if sec.AppArmor == "enabled" || sec.AppArmor == "active" {
			overview.AppArmorEnabled++
		}
	}

	writeJSON(w, http.StatusOK, overview)
}

// SecurityDevices handles GET /api/v1/security/devices.
func (h *Handlers) SecurityDevices(w http.ResponseWriter, r *http.Request) {
	snapshots, err := h.telemetry.GetAllLatestSnapshots(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to get telemetry"}`, http.StatusInternalServerError)
		return
	}

	devices, err := h.devices.List(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list devices"}`, http.StatusInternalServerError)
		return
	}

	// Build device ID → device map
	deviceMap := make(map[string]struct{ hostname, status string })
	for _, d := range devices {
		deviceMap[d.ID] = struct{ hostname, status string }{d.Hostname, string(d.Status)}
	}

	var result []deviceSecurityInfo
	for _, snap := range snapshots {
		sec := snap.Data.Security
		if sec == nil {
			continue
		}
		dev := deviceMap[snap.DeviceID]
		info := deviceSecurityInfo{
			DeviceID:        snap.DeviceID,
			Hostname:        dev.hostname,
			Status:          dev.status,
			SELinux:         sec.SELinux,
			AppArmor:        sec.AppArmor,
			FirewallStatus:  sec.FirewallStatus,
			FailedLogins24h: sec.FailedLogins24h,
			LoggedInUsers:   sec.LoggedInUsers,
			OpenPorts:       sec.OpenPorts,
		}
		if info.OpenPorts == nil {
			info.OpenPorts = []int{}
		}
		result = append(result, info)
	}

	if result == nil {
		result = []deviceSecurityInfo{}
	}
	writeJSON(w, http.StatusOK, result)
}
