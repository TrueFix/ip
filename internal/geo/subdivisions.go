package geo

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/TrueFix/myip/internal/assets"
	"golang.org/x/text/unicode/norm"
)

type subdivision struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}
type subdivisionKey struct{ country, name string }

var subdivisionCodes = loadSubdivisionCodes()

func loadSubdivisionCodes() map[subdivisionKey]string {
	b, err := assets.Files.ReadFile("subdivisions.json")
	if err != nil {
		panic(err)
	}
	var records []subdivision
	if err := json.Unmarshal(b, &records); err != nil {
		panic(err)
	}
	return indexSubdivisions(records)
}

func normalizeRegionName(name string) string {
	// ISO names may contain transliteration marks (Mahārāshtra) while GeoIP
	// providers use unaccented names (Maharashtra). Collisions fail closed.
	name = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(name))
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func indexSubdivisions(records []subdivision) map[subdivisionKey]string {
	index := make(map[subdivisionKey]string)
	for _, r := range records {
		// GeoIP's first subdivision is a first-level administrative division.
		// Do not confuse identically named districts with states/provinces.
		if len(r.Parent) != 2 || !strings.HasPrefix(r.Code, r.Parent+"-") || r.Name == "" {
			continue
		}
		key := subdivisionKey{r.Parent, normalizeRegionName(r.Name)}
		code := strings.TrimPrefix(r.Code, r.Parent+"-")
		if code == "" {
			continue
		}
		if old, exists := index[key]; exists && old != code {
			index[key] = ""
		} else if !exists {
			index[key] = code
		}
	}
	return index
}

func regionCodeFor(country, name string) *string {
	return str(subdivisionCodes[subdivisionKey{strings.ToUpper(strings.TrimSpace(country)), normalizeRegionName(name)}])
}
