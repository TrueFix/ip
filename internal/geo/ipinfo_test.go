package geo

import (
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/oschwald/maxminddb-golang"
)

func writeIPInfoFixture(t *testing.T, path string, asn uint) {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: "ipinfo_lite", IPVersion: 6, Description: map[string]string{"en": "Synthetic network fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	_, network, _ := net.ParseCIDR("81.2.69.0/24")
	if err := tree.Insert(network, mmdbtype.Map{"asn": mmdbtype.String("AS" + strconv.FormatUint(uint64(asn), 10)), "as_name": mmdbtype.String("Test Network"), "as_domain": mmdbtype.String("network.example")}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.WriteTo(f); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Verify(); err != nil {
		t.Fatal("generated fixture integrity:", err)
	}
}

func TestIPInfoAndAllSourceTiming(t *testing.T) {
	s := fixtureStore(t)
	ip := netip.MustParseAddr("81.2.69.142")
	baseline, err := s.Lookup(ip)
	if err != nil {
		t.Fatal(err)
	}
	asn := uint(65001)
	if baseline.Connection.ASN != nil {
		asn = *baseline.Connection.ASN
	}
	path := filepath.Join(t.TempDir(), "IPinfo.mmdb")
	writeIPInfoFixture(t, path, asn)
	s.ipinfo, err = OpenIPInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	s.fallbackCity, s.fallbackASN, err = OpenPair(filepath.Join(s.dir, "test-version"), "dbip")
	if err != nil {
		t.Fatal(err)
	}
	s.provider = "maxmind"
	s.ispFromASN = true
	normal, err := s.Lookup(ip)
	if err != nil {
		t.Fatal(err)
	}
	if normal.Connection.Domain == nil || *normal.Connection.Domain != "network.example" || normal.Connection.ISP == nil || *normal.Connection.ISP != "Test Network" || !normal.IPInfoUsed {
		t.Fatal("IPinfo not enriched")
	}
	if normal.Timings.DBIPCalled != (baseline.Connection.ASN == nil) || !normal.Timings.IPInfoCalled || !normal.Timings.MaxMindCalled {
		t.Fatal("normal path did not distinguish skipped city fallback from missing-ASN fallback")
	}
	all, err := s.LookupAllSources(ip)
	if err != nil {
		t.Fatal(err)
	}
	if all.City == nil || *all.City != *normal.City {
		t.Fatal("diagnostic probes changed primary selection")
	}
	if !all.Timings.MaxMindCalled || !all.Timings.DBIPCalled || !all.Timings.IPInfoCalled || all.Timings.Total <= 0 {
		t.Fatal("missing source timings")
	}
	header := all.Timings.ServerTiming()
	for _, name := range []string{"lookup;dur=", "maxmind;dur=", "dbip;dur=", "ipinfo;dur="} {
		if !strings.Contains(header, name) {
			t.Fatal("missing Server-Timing stage")
		}
	}
	b, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Timings") || strings.Contains(string(b), "IPInfoUsed") {
		t.Fatal("internal timings leaked into response schema")
	}
}

func TestEnrichNetworkSemantics(t *testing.T) {
	asn := uint(24560)
	for _, approx := range []bool{false, true} {
		r := Response{Connection: Connection{ASN: &asn, Org: str("Primary organization")}}
		if !enrichNetwork(&r, ipinfoRecord{ASN: "AS24560", Name: "Operator", Domain: "AIRTEL.IN"}, approx) {
			t.Fatal("matching ASN rejected")
		}
		if r.Connection.Domain == nil || *r.Connection.Domain != "airtel.in" || *r.Connection.Org != "Primary organization" {
			t.Fatal("bad network enrichment")
		}
		if (r.Connection.ISP != nil) != approx {
			t.Fatal("ISP approximation not explicit")
		}
	}
	r := Response{Connection: Connection{ASN: &asn}}
	if enrichNetwork(&r, ipinfoRecord{ASN: "AS13335", Name: "Other", Domain: "other.example"}, true) || r.Connection.Domain != nil || r.Connection.ISP != nil {
		t.Fatal("cross-ASN enrichment")
	}
	r = Response{}
	if !enrichNetwork(&r, ipinfoRecord{ASN: "AS24560", Name: "Operator", Domain: "https://bad.example/path"}, false) || r.Connection.Domain != nil || r.Connection.ASN == nil {
		t.Fatal("invalid domain not rejected or missing ASN not filled")
	}
}

func TestLiveIPInfoSample(t *testing.T) {
	path := os.Getenv("MYIP_TEST_IPINFO_FILE")
	if path == "" {
		t.Skip("set MYIP_TEST_IPINFO_FILE to validate an official Lite sample/database")
	}
	db, err := OpenIPInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}
