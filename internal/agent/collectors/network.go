package collectors

import (
	"context"
	gonet "net"

	"github.com/DesyncTheThird/rIOt/internal/models"
	psnet "github.com/shirou/gopsutil/v3/net"
)

type NetworkCollector struct{}

func (c *NetworkCollector) Name() string { return "network" }

func (c *NetworkCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.NetworkInfo{}

	ifaces, err := psnet.InterfacesWithContext(ctx)
	if err != nil {
		return info, err
	}

	ioCounters, _ := psnet.IOCountersWithContext(ctx, true)
	ioMap := make(map[string]psnet.IOCountersStat)
	for _, counter := range ioCounters {
		ioMap[counter.Name] = counter
	}

	for _, iface := range ifaces {
		if len(iface.Addrs) == 0 && len(iface.HardwareAddr) == 0 {
			continue
		}

		ni := models.NetworkInterface{
			Name:  iface.Name,
			MAC:   iface.HardwareAddr,
			State: "DOWN",
		}

		for _, flag := range iface.Flags {
			if flag == "up" {
				ni.State = "UP"
				break
			}
		}

		for _, addr := range iface.Addrs {
			ip, _, err := gonet.ParseCIDR(addr.Addr)
			if err != nil {
				continue
			}
			if ip.To4() != nil {
				ni.IPv4 = append(ni.IPv4, addr.Addr)
			} else {
				ni.IPv6 = append(ni.IPv6, addr.Addr)
			}
		}

		if counter, ok := ioMap[iface.Name]; ok {
			ni.BytesSent = counter.BytesSent
			ni.BytesRecv = counter.BytesRecv
		}

		info.Interfaces = append(info.Interfaces, ni)
	}

	return info, nil
}
