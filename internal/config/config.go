package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	Db_url string
	Current_user_name string
}

func Read() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	
	cfgData, err := os.ReadFile(homeDir + "/bootdev-aggregator/.gatorconfig.json")
	if err != nil {
		return Config{}, err
	}

	err = json.Unmarshal(cfgData, &cfg)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) SetUser(username string) error {
	c.Current_user_name = username
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgData, err := json.Marshal(c)
	if err != nil {
		return err
	}
	err = os.WriteFile(homeDir+"/bootdev-aggregator/.gatorconfig.json", cfgData, 0644)
	if err != nil {
		return err
	}
	return nil
}