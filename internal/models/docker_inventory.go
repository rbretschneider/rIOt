package models

import "time"

type DockerDiskUsage struct {
	Total       int64 `json:"total"`
	Reclaimable int64 `json:"reclaimable"`
}
type DockerInventoryTruncation struct {
	Images   bool `json:"images"`
	Volumes  bool `json:"volumes"`
	Networks bool `json:"networks"`
}
type DockerInventory struct {
	TruncatedKinds DockerInventoryTruncation  `json:"truncated_kinds"`
	Hash           string                     `json:"hash,omitempty"`
	Unchanged      bool                       `json:"unchanged,omitempty"`
	DiskUsage      map[string]DockerDiskUsage `json:"disk_usage,omitempty"`
	SizesAvailable bool                       `json:"sizes_available"`
	CollectedAt    time.Time                  `json:"collected_at"`
	Images         []DockerImage              `json:"images"`
	Volumes        []DockerVolume             `json:"volumes"`
	Networks       []DockerNetwork            `json:"networks"`
	Truncated      bool                       `json:"truncated,omitempty"`
}
type DockerImage struct {
	Digests    []string  `json:"digests,omitempty"`
	Dangling   bool      `json:"dangling"`
	ID         string    `json:"id"`
	Tags       []string  `json:"tags"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"created_at"`
	InUse      bool      `json:"in_use"`
	Containers int64     `json:"containers"`
}
type DockerVolume struct {
	UsedBy     []string `json:"used_by"`
	Mountpoint string   `json:"mountpoint,omitempty"`
	Project    string   `json:"project,omitempty"`
	Name       string   `json:"name"`
	Driver     string   `json:"driver"`
	Size       int64    `json:"size"`
	CreatedAt  string   `json:"created_at"`
	InUse      bool     `json:"in_use"`
	Anonymous  bool     `json:"anonymous"`
}
type DockerNetwork struct {
	Containers []string       `json:"containers"`
	Internal   bool           `json:"internal"`
	Subnets    []DockerSubnet `json:"subnets,omitempty"`
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Driver     string         `json:"driver"`
	Scope      string         `json:"scope"`
	CreatedAt  time.Time      `json:"created_at"`
	InUse      bool           `json:"in_use"`
	Builtin    bool           `json:"builtin"`
}
type DockerSubnet struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
}
