package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Game        GameConfig
	Map         MapConfig
	UnitsByID   map[string]UnitConfig
	RegionsByID map[string]RegionConfig
	PathsByID   map[string]PathConfig
}

func Load(unitsPath, mapPath string) (*Config, error) {
	gc, err := loadGame(unitsPath)
	if err != nil {
		return nil, fmt.Errorf("load units: %w", err)
	}
	mc, err := loadMap(mapPath)
	if err != nil {
		return nil, fmt.Errorf("load map: %w", err)
	}

	c := &Config{
		Game:        gc,
		Map:         mc,
		UnitsByID:   make(map[string]UnitConfig, len(gc.Units)),
		RegionsByID: make(map[string]RegionConfig, len(mc.Regions)),
		PathsByID:   make(map[string]PathConfig, len(mc.Paths)),
	}
	for _, u := range gc.Units {
		c.UnitsByID[u.ID] = u
	}
	for _, r := range mc.Regions {
		c.RegionsByID[r.ID] = r
	}
	for _, p := range mc.Paths {
		c.PathsByID[p.ID] = p
	}
	return c, nil
}

func loadGame(path string) (GameConfig, error) {
	var gc GameConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return gc, err
	}
	return gc, json.Unmarshal(data, &gc)
}

func loadMap(path string) (MapConfig, error) {
	var mc MapConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return mc, err
	}
	return mc, json.Unmarshal(data, &mc)
}
