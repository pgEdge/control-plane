package postgres

import (
	"fmt"
	"strings"
)

type DSN struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	SSLCert         string
	SSLKey          string
	SSLRootCert     string
	ApplicationName string
}

func (d *DSN) String() string {
	var fields []string
	if d.Host != "" {
		fields = append(fields, fmt.Sprintf("host=%s", d.Host))
	}
	if d.Port != 0 {
		fields = append(fields, fmt.Sprintf("port=%d", d.Port))
	}
	if d.User != "" {
		fields = append(fields, fmt.Sprintf("user=%s", d.User))
	}
	if d.Password != "" {
		fields = append(fields, fmt.Sprintf("password=%s", d.Password))
	}
	if d.DBName != "" {
		fields = append(fields, fmt.Sprintf("dbname=%s", d.DBName))
	}
	if d.SSLCert != "" {
		fields = append(fields, fmt.Sprintf("sslcert=%s", d.SSLCert))
	}
	if d.SSLKey != "" {
		fields = append(fields, fmt.Sprintf("sslkey=%s", d.SSLKey))
	}
	if d.SSLRootCert != "" {
		fields = append(fields, fmt.Sprintf("sslrootcert=%s", d.SSLRootCert))
	}
	if d.ApplicationName != "" {
		fields = append(fields, fmt.Sprintf("application_name=%s", d.ApplicationName))
	} else {
		fields = append(fields, "application_name=control-plane")
	}
	return strings.Join(fields, " ")
}
