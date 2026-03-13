package postgres

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

type DSN struct {
	Hosts              []string
	Ports              []int
	User               string
	Password           string
	DBName             string
	SSLCert            string
	SSLKey             string
	SSLRootCert        string
	Service            string
	ApplicationName    string
	TargetSessionAttrs string
	Extra              map[string]string
}

func (d *DSN) Host() string {
	return strings.Join(d.Hosts, ",")
}

func (d *DSN) Port() string {
	ports := make([]string, len(d.Ports))
	for i, port := range d.Ports {
		ports[i] = strconv.Itoa(port)
	}
	return strings.Join(ports, ",")
}

func (d *DSN) Fields() []string {
	var fields []string
	addField := func(key, value string) {
		if value == "" {
			return
		}

		var buf strings.Builder
		buf.WriteString(key)
		buf.WriteString("=")
		buf.WriteString(value)

		fields = append(fields, buf.String())
	}

	addField("host", d.Host())
	addField("port", d.Port())
	addField("user", d.User)
	addField("password", d.Password)
	addField("dbname", d.DBName)
	addField("sslcert", d.SSLCert)
	addField("sslkey", d.SSLKey)
	addField("sslrootcert", d.SSLRootCert)
	addField("service", d.Service)
	addField("application_name", d.ApplicationName)
	addField("target_session_attrs", d.TargetSessionAttrs)

	// Sort extra keys for deterministic output
	for _, key := range slices.Sorted(maps.Keys(d.Extra)) {
		addField(key, d.Extra[key])
	}

	return fields
}

func (d *DSN) String() string {
	return strings.Join(d.Fields(), " ")
}
