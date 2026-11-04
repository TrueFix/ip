package geo

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/TrueFix/myip/internal/assets"
)

type Response struct {
	IP            string        `json:"ip"`
	Success       bool          `json:"success"`
	Type          string        `json:"type"`
	Continent     *string       `json:"continent"`
	ContinentCode *string       `json:"continent_code"`
	Country       *string       `json:"country"`
	CountryCode   *string       `json:"country_code"`
	Region        *string       `json:"region"`
	RegionCode    *string       `json:"region_code"`
	City          *string       `json:"city"`
	Latitude      *float64      `json:"latitude"`
	Longitude     *float64      `json:"longitude"`
	IsEU          *bool         `json:"is_eu"`
	CallingCode   *string       `json:"calling_code"`
	Capital       *string       `json:"capital"`
	Borders       *string       `json:"borders"`
	Flag          Flag          `json:"flag"`
	Connection    Connection    `json:"connection"`
	Timezone      Timezone      `json:"timezone"`
	Readme        string        `json:"readme"`
	Timings       LookupTimings `json:"-"`
	IPInfoUsed    bool          `json:"-"`
}
type Flag struct {
	Image   *string `json:"img"`
	Emoji   *string `json:"emoji"`
	Unicode *string `json:"emoji_unicode"`
}
type Connection struct {
	ASN    *uint   `json:"asn"`
	Org    *string `json:"org"`
	ISP    *string `json:"isp"`
	Domain *string `json:"domain"`
}
type Timezone struct {
	ID     *string `json:"id"`
	Abbr   *string `json:"abbr"`
	IsDST  *bool   `json:"is_dst"`
	Offset *int    `json:"offset"`
	UTC    *string `json:"utc"`
}
type Country struct {
	CCA2    string   `json:"cca2"`
	CCA3    string   `json:"cca3"`
	Capital []string `json:"capital"`
	Borders []string `json:"borders"`
	IDD     struct {
		Root     string   `json:"root"`
		Suffixes []string `json:"suffixes"`
	} `json:"idd"`
}

var countries map[string]Country
var singleTimezones map[string]string

func init() {
	b, err := assets.Files.ReadFile("countries.json")
	if err != nil {
		panic(err)
	}
	var list []Country
	if err := json.Unmarshal(b, &list); err != nil {
		panic(err)
	}
	countries = make(map[string]Country, len(list))
	for _, c := range list {
		countries[c.CCA2] = c
	}
	zones, err := assets.Files.ReadFile("zone.tab")
	if err != nil {
		panic(err)
	}
	singleTimezones = make(map[string]string)
	for _, line := range strings.Split(string(zones), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		code := fields[0]
		if _, exists := singleTimezones[code]; exists {
			singleTimezones[code] = ""
		} else {
			singleTimezones[code] = fields[2]
		}
	}
}

func str(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func enrich(r *Response, now time.Time) {
	if r.CountryCode != nil {
		code := *r.CountryCode
		if r.Region != nil && (r.RegionCode == nil || *r.RegionCode == "") {
			r.RegionCode = regionCodeFor(code, *r.Region)
		}
		if c, ok := countries[code]; ok {
			r.Capital = str(strings.Join(c.Capital, ", "))
			calling := strings.TrimPrefix(c.IDD.Root, "+")
			if len(c.IDD.Suffixes) == 1 {
				calling += c.IDD.Suffixes[0]
			}
			r.CallingCode = str(calling)
			borders := []string{}
			for _, b := range c.Borders {
				for _, other := range countries {
					if other.CCA3 == b {
						borders = append(borders, other.CCA2)
						break
					}
				}
			}
			sort.Strings(borders)
			joined := strings.Join(borders, ",")
			r.Borders = &joined
		}
		if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			a, b := rune(0x1F1E6)+rune(code[0]-'A'), rune(0x1F1E6)+rune(code[1]-'A')
			r.Flag.Emoji = str(string([]rune{a, b}))
			r.Flag.Unicode = str(fmt.Sprintf("U+%X U+%X", a, b))
			path := "flags/" + strings.ToLower(code) + ".svg"
			if _, err := assets.Files.ReadFile(path); err == nil {
				r.Flag.Image = str("/" + path)
			}
		}
		// Membership is country metadata, independent of the geolocation provider.
		eu := strings.Contains("|AT|BE|BG|HR|CY|CZ|DK|EE|FI|FR|DE|GR|HU|IE|IT|LV|LT|LU|MT|NL|PL|PT|RO|SK|SI|ES|SE|", "|"+code+"|")
		r.IsEU = &eu
	}
	if r.Timezone.ID != nil {
		if loc, err := time.LoadLocation(*r.Timezone.ID); err == nil {
			t := now.In(loc)
			abbr, offset := t.Zone()
			dst := t.IsDST()
			sign, n := "+", offset
			if n < 0 {
				sign = "-"
				n = -n
			}
			r.Timezone.Abbr = str(abbr)
			r.Timezone.IsDST = &dst
			r.Timezone.Offset = &offset
			r.Timezone.UTC = str(fmt.Sprintf("%s%02d:%02d", sign, n/3600, (n%3600)/60))
		}
	}
}
