package main

// hookage.go — how long the hook has been loaded, for the startup splash.
//
// push_hook.c ignores the display mode for BOOT_GRACE_SECS after Push3 loads
// it. A splash that runs inside that time is never drawn. The hook sits in
// Push3 and loads with it, so Push3's age is the hook's age, give or take a
// moment. Reading it from /proc needs no change to the hook.

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	hookGrace  = 8 * time.Second // BOOT_GRACE_SECS in push_hook.c
	hookLoaded = time.Second     // old enough that its shm setup is done
	clockTick  = 100             // /proc/<pid>/stat counts in 1/100 s on Linux x86
)

// startTicks returns field 22 (starttime) of a /proc/<pid>/stat line. The
// command name is field 2 and can hold spaces and brackets, so count from
// the last ")".
func startTicks(stat string) (uint64, bool) {
	i := strings.LastIndex(stat, ")")
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(stat[i+1:]) // f[0] is field 3
	if len(f) < 20 {
		return 0, false
	}
	n, err := strconv.ParseUint(f[19], 10, 64) // field 22
	return n, err == nil
}

// hookAge is the age of the youngest process named Push3. ok is false when
// there is none yet.
func hookAge() (age time.Duration, ok bool) {
	up, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	secs, err := strconv.ParseFloat(strings.Fields(string(up))[0], 64)
	if err != nil {
		return 0, false
	}
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + d.Name() + "/comm")
		if err != nil || strings.TrimSpace(string(comm)) != "Push3" {
			continue
		}
		stat, err := os.ReadFile("/proc/" + d.Name() + "/stat")
		if err != nil {
			continue
		}
		t, good := startTicks(string(stat))
		if !good {
			continue
		}
		a := time.Duration((secs - float64(t)/clockTick) * float64(time.Second))
		if !ok || a < age {
			age, ok = a, true
		}
	}
	return age, ok
}

// waitHookAge waits until Push3 is at least min old, or timeout passes.
// It returns at once when Push3 is already old enough (push-manager
// restarted alone).
func waitHookAge(min, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if age, ok := hookAge(); ok && age >= min {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
