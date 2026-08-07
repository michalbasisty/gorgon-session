package server

import "strings"

// zoneCoordsFallback supplies approximate world-map positions for zones the
// CDN's areas.json does not publish X/Y for (currently all of them).
//
// Project Gorgon zones are portal-connected instances, not tiles on a world
// grid, so no authoritative inter-zone coordinates exist anywhere (CDN, wiki,
// community tools). These values were derived from the wiki's directional
// zone relationships ("X is north of Y") on a consistent 0-1000 grid
// (south→north = y up, west→east = x right). Scale is arbitrary — they are
// used for RELATIVE "nearest first" sorting only, not navigation.
//
// ponytail: static wiki-derived table; refresh if the wiki layout changes.
var zoneCoordsFallback = map[string][2]float64{
	"anagoge island":                 {150, 50},
	"serbule hills":                  {200, 150},
	"serbule":                        {200, 300},
	"caves under serbule":            {200, 300},
	"carpal tunnels":                 {200, 300},
	"myconian caverns":               {200, 300},
	"khyrulek's crypt":               {200, 300},
	"phantom ilmari desert":          {200, 350},
	"eltibule":                       {200, 450},
	"dungeons beneath eltibule":      {200, 450},
	"sun vale":                       {500, 350},
	"caves beneath sun vale":         {500, 350},
	"winter nexus":                   {500, 350},
	"red wing casino":                {100, 450},
	"kur mountains":                  {250, 600},
	"caves beneath kur mountains":    {250, 600},
	"kur tower":                      {250, 600},
	"ilmari desert":                  {550, 650},
	"dungeons beneath ilmari desert": {550, 650},
	"rahu":                           {700, 750},
	"rahu sewers":                    {700, 750},
	"gazluk":                         {350, 800},
	"gazluk plateau":                 {350, 800},
	"gazluk keep":                    {350, 800},
	"new prestonbule":                {350, 800},
	"snowblood shadow":               {350, 800},
	"gazluk shadow":                  {350, 800},
	"caves beneath gazluk":           {350, 800},
	"existential planes":             {600, 980},
	"guild hall":                     {200, 300},
	"player apartment":               {200, 300},
	"staging area":                   {180, 270},
	"povus":                          {550, 900},
	"povus caves":                    {550, 900},
	"vidaria":                        {300, 920},
	"beneath vidaria":                {300, 920},
	"statehelm":                      {150, 970},
	"statehelm undercity":            {150, 970},
	"statehelm depths":               {150, 970},
	"fae realm":                      {450, 500},
}

// fallbackAreaCoords resolves an area name against zoneCoordsFallback,
// including dungeon names that embed a parent zone ("Serbule Crypt" →
// Serbule, "Rahu Sewer" → Rahu).
func fallbackAreaCoords(area string) (float64, float64, bool) {
	name := strings.ToLower(strings.TrimSpace(area))
	if xy, ok := zoneCoordsFallback[name]; ok {
		return xy[0], xy[1], true
	}
	best := ""
	for k := range zoneCoordsFallback {
		if strings.Contains(name, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return 0, 0, false
	}
	xy := zoneCoordsFallback[best]
	return xy[0], xy[1], true
}
