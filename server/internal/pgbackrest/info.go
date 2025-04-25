package pgbackrest

import (
	"encoding/json"
	"fmt"
	"time"
)

type InfoOutput []*StanzaInfo

func (i InfoOutput) Stanza(name string) *StanzaInfo {
	for _, stanza := range i {
		if stanza.Name == name {
			return stanza
		}
	}
	return nil
}

type StanzaInfo struct {
	Name   string       `json:"name"`
	Status Status       `json:"status"`
	DB     []Database   `json:"db"`
	Backup []BackupInfo `json:"backup"`
}

type Status struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Database struct {
	Version  string `json:"version"`
	SystemID int64  `json:"system-id"`
}

type BackupInfo struct {
	Label     string       `json:"label"`
	Type      string       `json:"type"`
	Timestamp BackupTime   `json:"timestamp"`
	Archive   ArchiveRange `json:"archive"`
	Database  DBRef        `json:"database"`
	Info      BackupSize   `json:"info"`
}

type BackupTime struct {
	Start int64 `json:"start"`
	Stop  int64 `json:"stop"`
}

func (b BackupTime) StartTime() time.Time {
	return time.Unix(b.Start, 0)
}

func (b BackupTime) StopTime() time.Time {
	return time.Unix(b.Stop, 0)
}

type ArchiveRange struct {
	Start string `json:"start"`
	Stop  string `json:"stop"`
}

type DBRef struct {
	ID int `json:"id"`
}

type BackupSize struct {
	Size  int64 `json:"size"`
	Delta int64 `json:"delta"`
}

func ParseInfoOutput(output []byte) (InfoOutput, error) {
	var info InfoOutput
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse pgbackrest info output: %w", err)
	}
	return info, nil
}
