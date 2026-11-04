package geo

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

type ipinfoRecord struct {
	ASN    string `maxminddb:"asn"`
	Name   string `maxminddb:"as_name"`
	Domain string `maxminddb:"as_domain"`
}

// OpenIPInfo validates both MMDB integrity and the Lite schema. It intentionally
// does not accept arbitrary ASN databases with different field semantics.
func OpenIPInfo(path string) (*maxminddb.Reader, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	fail := func(reason string) (*maxminddb.Reader, error) {
		db.Close()
		return nil, errors.New("invalid IPinfo Lite database: " + reason)
	}
	if !strings.Contains(strings.ToLower(db.Metadata.DatabaseType), "ipinfo") {
		return fail("unexpected database type")
	}
	if err := db.Verify(); err != nil {
		return fail("MMDB integrity check failed")
	}
	iter := db.Networks()
	valid := false
	for iter.Next() {
		var r ipinfoRecord
		_, err := iter.Network(&r)
		if err != nil {
			return fail("record schema mismatch")
		}
		if asnNumber(r.ASN) > 0 && r.Name != "" && r.Domain != "" {
			valid = true
			break
		}
	}
	if iter.Err() != nil || !valid {
		return fail("no valid Lite network records")
	}
	return db, nil
}

func asnNumber(s string) uint {
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "AS"), 10, 32)
	if err != nil {
		return 0
	}
	return uint(n)
}

func validNetworkDomain(s string) bool {
	if s == "" || len(s) > 253 || net.ParseIP(s) != nil || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// Network metadata is joined only when ASNs agree, never across conflicting
// network owners. ISP_FROM_ASN is explicitly an operator-name approximation.
func enrichNetwork(r *Response, record ipinfoRecord, ispFromASN bool) bool {
	asn := asnNumber(record.ASN)
	if asn == 0 {
		return false
	}
	if r.Connection.ASN != nil && *r.Connection.ASN != asn {
		return false
	}
	if r.Connection.ASN == nil {
		r.Connection.ASN = &asn
	}
	if r.Connection.Org == nil {
		r.Connection.Org = str(record.Name)
	}
	domain := strings.ToLower(strings.TrimSpace(record.Domain))
	if validNetworkDomain(domain) {
		r.Connection.Domain = str(domain)
	}
	if ispFromASN {
		r.Connection.ISP = str(record.Name)
	}
	return true
}
