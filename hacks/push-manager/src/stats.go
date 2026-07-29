package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SystemStats holds all collected system metrics.
// Note: Battery info is NOT included — Push 3's battery state is managed by
// the XMOS firmware via a custom MIDI SysEx protocol and is not exposed through
// any standard Linux interface (/sys/class/power_supply is empty on Push 3).
type SystemStats struct {
	CPUPercent      float64       `json:"cpu_percent"`
	TopProcs        []ProcStat    `json:"top_procs,omitempty"`
	Memory          *MemStats     `json:"memory,omitempty"`
	Disk            *DiskStats    `json:"disk,omitempty"`
	IPAddresses     []string      `json:"ip_addresses"`
	HotspotPassword string        `json:"hotspot_password,omitempty"`
	Battery         *BatteryStats `json:"battery,omitempty"`
	UptimeSeconds   float64       `json:"uptime_seconds"`
}

// ProcStat is per-process CPU usage for display in the stats panel.
type ProcStat struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
}

type MemStats  struct { Total, Used, Free uint64 }
type DiskStats struct { Total, Used, Free uint64 }
type BatteryStats struct {
	Percent int    `json:"percent"`
	Status  string `json:"status"`
}

func (m *MemStats)  MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"total":%d,"used":%d,"free":%d}`, m.Total, m.Used, m.Free)), nil
}
func (d *DiskStats) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"total":%d,"used":%d,"free":%d}`, d.Total, d.Used, d.Free)), nil
}

// watchedProcs: processes shown under the CPU stat row.
// label = display name; match = substring searched in /proc/<pid>/cmdline.
// Order determines display order. First PID whose cmdline contains match wins.
var watchedProcs = []struct{ label, match string }{
	{"Ableton Index", "Ableton Index"},
	{"Live",          "/opt/push3/Live"},
	{"Push3",         "/opt/push3/Push3"},
	{"push-manager",  "push-manager"},
}

func collectStats(diskPath string) SystemStats {
	s := SystemStats{}

	// Find PIDs for watched processes once (before the 250ms window).
	pids := findWatchedPIDs()

	// Sample 1: overall CPU ticks + per-process ticks.
	cpu1, _ := readCPUSample()
	pt1 := map[string]uint64{}
	for label, pid := range pids {
		if t, err := readProcTicks(pid); err == nil {
			pt1[label] = t
		}
	}

	time.Sleep(250 * time.Millisecond)

	// Sample 2.
	cpu2, _ := readCPUSample()
	pt2 := map[string]uint64{}
	for label, pid := range pids {
		if t, err := readProcTicks(pid); err == nil {
			pt2[label] = t
		}
	}

	dIdle := float64(cpu2[3] + cpu2[4] - cpu1[3] - cpu1[4])
	dTotal := float64(0)
	for i := range cpu1 {
		dTotal += float64(cpu2[i] - cpu1[i])
	}
	if dTotal > 0 {
		pct := (1 - dIdle/dTotal) * 100
		s.CPUPercent = float64(int(pct*10+0.5)) / 10
		for _, wp := range watchedProcs {
			t1, ok1 := pt1[wp.label]
			t2, ok2 := pt2[wp.label]
			if ok1 && ok2 && t2 >= t1 {
				p := float64(t2-t1) / dTotal * 100
				s.TopProcs = append(s.TopProcs, ProcStat{
					Name: wp.label,
					CPU:  float64(int(p*10+0.5)) / 10,
				})
			}
		}
	}

	s.Memory, _ = readMemInfo()
	s.Disk, _ = diskInfo(diskPath)
	s.IPAddresses = getLocalIPs()
	s.HotspotPassword, _ = hotspotPassword()
	s.Battery, _ = batteryInfo()
	s.UptimeSeconds, _ = uptimeSeconds()
	return s
}

// readCPUSample reads the 7 CPU time fields from /proc/stat (user nice sys idle iowait irq softirq).
func readCPUSample() ([7]uint64, error) {
	var v [7]uint64
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return v, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 8 {
			break
		}
		for i := range v {
			v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
		}
		return v, nil
	}
	return v, fmt.Errorf("no cpu line")
}

// findWatchedPIDs scans /proc for PIDs whose cmdline matches each watched process.
func findWatchedPIDs() map[string]int {
	result := map[string]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return result
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		// cmdline is NUL-separated; replace to make matching easy.
		cmd := strings.ReplaceAll(string(raw), "\x00", " ")
		for _, wp := range watchedProcs {
			if _, found := result[wp.label]; found {
				continue
			}
			if strings.Contains(cmd, wp.match) {
				result[wp.label] = pid
			}
		}
	}
	return result
}

// readProcTicks returns utime+stime (in clock ticks) from /proc/<pid>/stat.
func readProcTicks(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// /proc/pid/stat: pid (comm) state ppid ... utime stime ...
	// comm can contain spaces/parens; find the last ')' to skip it safely.
	s := string(data)
	end := strings.LastIndex(s, ")")
	if end < 0 {
		return 0, fmt.Errorf("bad /proc/%d/stat", pid)
	}
	fields := strings.Fields(s[end+1:])
	// After ')': [0]=state [1]=ppid ... [11]=utime [12]=stime
	if len(fields) < 13 {
		return 0, fmt.Errorf("short /proc/%d/stat", pid)
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	return utime + stime, nil
}

// hotspotPassword reads the Wi-Fi hotspot password from Push's preferences JSON.
// Checks all versioned PushPreferences.json files; returns first non-empty password found.
func hotspotPassword() (string, error) {
	matches, err := filepath.Glob("/data/.config/Ableton/Live */PushPreferences.json")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no PushPreferences.json found")
	}
	var prefs struct {
		HotspotPassword string `json:"hotspot_password"`
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &prefs); err != nil {
			continue
		}
		if prefs.HotspotPassword != "" {
			return prefs.HotspotPassword, nil
		}
	}
	return "", fmt.Errorf("hotspot_password not found in any PushPreferences.json")
}


func readMemInfo() (*MemStats, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	vals := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(parts[1], 10, 64)
		vals[strings.TrimSuffix(parts[0], ":")] = val * 1024
	}
	if vals["MemTotal"] == 0 {
		return nil, fmt.Errorf("no MemTotal")
	}
	avail := vals["MemAvailable"]
	return &MemStats{Total: vals["MemTotal"], Used: vals["MemTotal"] - avail, Free: avail}, nil
}

func diskInfo(path string) (*DiskStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	free := stat.Bavail * bsize
	return &DiskStats{Total: total, Free: free, Used: total - free}, nil
}

func getLocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				ips = append(ips, iface.Name+": "+ip.String())
			}
		}
	}
	return ips
}

func batteryInfo() (*BatteryStats, error) {
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		base := filepath.Join("/sys/class/power_supply", e.Name())
		typ, _ := os.ReadFile(filepath.Join(base, "type"))
		if strings.TrimSpace(string(typ)) == "Mains" {
			continue
		}
		capData, err := os.ReadFile(filepath.Join(base, "capacity"))
		if err != nil {
			continue
		}
		pct, err := strconv.Atoi(strings.TrimSpace(string(capData)))
		if err != nil {
			continue
		}
		status := "Unknown"
		if data, err := os.ReadFile(filepath.Join(base, "status")); err == nil {
			status = strings.TrimSpace(string(data))
		}
		return &BatteryStats{Percent: pct, Status: status}, nil
	}
	return nil, fmt.Errorf("no battery")
}

func uptimeSeconds() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty")
	}
	return strconv.ParseFloat(fields[0], 64)
}
