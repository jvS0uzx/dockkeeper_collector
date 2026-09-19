package scan

import (
	"bufio"
	"os"
	"strings"
)

func ARPTable() map[string]string {
	file, err := os.Open("/proc/net/arp")
	if err != nil {
		return map[string]string{}
	}
	defer file.Close()

	macByIP := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Scan()

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		ip, mac := fields[0], strings.ToLower(fields[3])
		if mac == "00:00:00:00:00:00" {
			continue
		}
		macByIP[ip] = mac
	}
	return macByIP
}
