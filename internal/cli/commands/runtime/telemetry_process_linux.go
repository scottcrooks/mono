//go:build linux

package runtimecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// inspectTelemetryForward trusts only an exact kubectl command whose six sockets
// belong to that process. Any inaccessible or ambiguous proc data is rejected.
func inspectTelemetryForward(pid int) (string, telemetryPorts, bool) {
	var ports telemetryPorts
	base := fmt.Sprintf("/proc/%d", pid)
	exe, err := os.Readlink(base + "/exe")
	if err != nil || filepath.Base(exe) != "kubectl" {
		return "", ports, false
	}
	data, err := os.ReadFile(base + "/cmdline")
	if err != nil {
		return "", ports, false
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if len(args) != 15 || args[1] != "--context" || args[2] == "" || args[3] != "-n" || args[4] != telemetryNamespace || args[5] != "port-forward" || args[6] != "--address" || args[7] != "127.0.0.1" || args[8] != "service/lgtm" {
		return "", ports, false
	}
	// Six mappings follow the nine command fields.
	return inspectTelemetryMappings(base, args, ports)
}

func inspectTelemetryMappings(base string, args []string, ports telemetryPorts) (string, telemetryPorts, bool) {
	if len(args) != 15 {
		return "", ports, false
	}
	for i, arg := range args[9:] {
		pieces := strings.Split(arg, ":")
		if len(pieces) != 2 {
			return "", ports, false
		}
		local, e1 := strconv.Atoi(pieces[0])
		target, e2 := strconv.Atoi(pieces[1])
		if e1 != nil || e2 != nil || target != defaultTelemetryPorts[i] {
			return "", ports, false
		}
		ports[i] = local
	}
	if ports.validate() != nil {
		return "", ports, false
	}
	entries, err := os.ReadDir(base + "/fd")
	if err != nil {
		return "", ports, false
	}
	inodes := map[string]bool{}
	for _, entry := range entries {
		link, e := os.Readlink(base + "/fd/" + entry.Name())
		if os.IsNotExist(e) {
			// An unrelated short-lived connection can disappear during the
			// scan. Missing a required listener still fails the ownership check.
			continue
		}
		if e != nil {
			return "", ports, false
		}
		if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	owned := map[int]bool{}
	for _, tableName := range []string{"tcp", "tcp6"} {
		table, err := os.ReadFile(base + "/net/" + tableName)
		if err != nil {
			return "", ports, false
		}
		for _, line := range strings.Split(string(table), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			addr := strings.Split(fields[1], ":")
			if len(addr) != 2 {
				continue
			}
			p, e := strconv.ParseUint(addr[1], 16, 16)
			if e != nil {
				continue
			}
			for _, port := range ports {
				if int(p) != port {
					continue
				}
				if tableName != "tcp" || addr[0] != "0100007F" || !inodes[fields[9]] {
					return "", ports, false
				}
				owned[port] = true
			}
		}
	}
	for _, port := range ports {
		if !owned[port] {
			return "", ports, false
		}
	}
	return args[2], ports, true
}

// telemetryForwardPIDs enumerates candidates; inspectTelemetryForward still
// verifies the command and ownership of every listener before reuse.
func telemetryForwardPIDs() []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}
