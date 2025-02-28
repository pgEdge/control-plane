package hba

import (
	"fmt"
)

type EntryType string

const (
	EntryTypeLocal           EntryType = "local"
	EntryTypeHost            EntryType = "host"
	EntryTypeHostSSL         EntryType = "hostssl"
	EntryTypeHostNoSSL       EntryType = "hostnossl"
	EntryTypeHostGSSEnc      EntryType = "hostgssenc"
	EntryTypeHostNoGSSEnc    EntryType = "hostnogssenc"
	EntryTypeInclude         EntryType = "include"
	EntryTypeIncludeIfExists EntryType = "include_if_exists"
	EntryTypeIncludeDir      EntryType = "include_dir"
)

type AuthMethod string

const (
	AuthMethodTrust       AuthMethod = "trust"
	AuthMethodReject      AuthMethod = "reject"
	AuthMethodMD5         AuthMethod = "md5"
	AuthMethodScramSHA256 AuthMethod = "scram-sha-256"
	AuthMethodPassword    AuthMethod = "password"
	AuthMethodGSS         AuthMethod = "gss"
	AuthMethodSSPI        AuthMethod = "sspi"
	AuthMethodIdent       AuthMethod = "ident"
	AuthMethodPeer        AuthMethod = "peer"
	AuthMethodLDAP        AuthMethod = "ldap"
	AuthMethodRadius      AuthMethod = "radius"
	AuthMethodCert        AuthMethod = "cert"
	AuthMethodPAM         AuthMethod = "pam"
	AuthMethodBSD         AuthMethod = "bsd"
)

const (
	AuthOptionVerifyCA   string = "clientcert=verify-ca"
	AuthOptionVerifyFull string = "clientcert=verify-full"
)

type Entry struct {
	Type        EntryType
	Database    string
	User        string
	Address     string
	Mask        string
	AuthMethod  AuthMethod
	AuthOptions string
	IncludePath string
}

func (e Entry) String() string {
	var entry string
	switch e.Type {
	case EntryTypeInclude, EntryTypeIncludeIfExists, EntryTypeIncludeDir:
		return fmt.Sprintf("%-17s %s", e.Type, e.IncludePath)
	default:
		entry = fmt.Sprintf("%-7s %-15s %-15s %-23s", e.Type, e.Database, e.User, e.Address)
	}
	if e.Mask != "" {
		entry += fmt.Sprintf(" %-23s", e.Mask)
	}
	if e.AuthOptions != "" {
		entry += fmt.Sprintf(" %s %s", e.AuthMethod, e.AuthOptions)
	} else {
		entry += fmt.Sprintf(" %s", e.AuthMethod)
	}
	return entry
}
