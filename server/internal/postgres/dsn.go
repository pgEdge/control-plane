package postgres

import (
	"fmt"
	"strconv"
	"strings"
)

type DSN struct {
	Hosts           []string
	Ports           []int
	User            string
	Password        string
	DBName          string
	SSLCert         string
	SSLKey          string
	SSLRootCert     string
	ApplicationName string
	Extra           map[string]string
}

func (d *DSN) String() string {
	var fields []string
	if len(d.Hosts) > 0 {
		host := strings.Join(d.Hosts, ",")
		fields = append(fields, fmt.Sprintf("host=%s", host))
	}
	if len(d.Ports) > 0 {
		ports := make([]string, len(d.Ports))
		for i, port := range d.Ports {
			ports[i] = strconv.Itoa(port)
		}
		port := strings.Join(ports, ",")
		fields = append(fields, fmt.Sprintf("port=%s", port))
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
	for key, value := range d.Extra {
		fields = append(fields, fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(fields, " ")
}
