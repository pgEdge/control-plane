package systemd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func readTotalMemory() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	memTotal, err := extractMemTotal(f)
	if err != nil {
		return 0, fmt.Errorf("failed to extract MemTotal from /proc/meminfo: %w", err)
	}
	return memTotal, nil
}

func extractMemTotal(r io.Reader) (uint64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[0] != "MemTotal:" {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemTotal: %w", err)
		}
		return kb * 1024, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, errors.New("MemTotal not found")
}
