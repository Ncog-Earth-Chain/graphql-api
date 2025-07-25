package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"

	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/cryptod"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

// Load provides a loaded configuration for NCOGEarthChain API server.
func Load() (*Config, error) {
	// Get the config reader
	var config Config
	attachCliFlags(&config)

	cfg, err := readConfigFile()
	if err != nil {
		return nil, err
	}

	// prep the container and try to unmarshal
	// the config file into the config structure
	if err = cfg.Unmarshal(&config, setupConfigUnmarshaler); err != nil {
		log.Println("can not extract API server configuration")
		log.Println(err.Error())
		return nil, err
	}

	// try to load the logo map file
	loadErc20LogMap(&config)

	// return the final config
	return &config, nil
}

// attachCliFlags connects CLI flags to certain configuration options.
func attachCliFlags(cfg *Config) {
	flag.Uint64Var(&cfg.RepoCommand.BlockScanReScan, keyConfigCmdBlockScanReScan, defBlockScanRescanDepth, "How many blocks are re-scanned on the server start.")
	flag.StringVar(&cfg.RepoCommand.RestoreStake, keyConfigCmdRestoreStake, "", "Owner of the stake to be restored.")
}

// readConfigFile reads the config file and provides instance
// of the loaded configuration.
func readConfigFile() (*viper.Viper, error) {
	// inform about tokens loading
	log.Printf("loading app configuration")

	// Get the config reader
	cfg := reader()

	// set default values
	applyDefaults(cfg)

	// Try to read the file
	if err := cfg.ReadInConfig(); err != nil {
		// is this an error notifying missing config file?
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Config file was found but another error was produced
			log.Printf("can not read the server configuration")
			return nil, err
		}

		// Config file not found; ignore the error, we may not need the config file
		log.Print("configuration file not found, using default values")
	}

	return cfg, nil
}

// loadErc20LogMap loads the map of ERC20 token logos.
func loadErc20LogMap(cfg *Config) {
	// is there any path at all?
	if cfg.TokenLogoFilePath == "" {
		log.Print("ERC20 tokens map file path not available")
		return
	}

	// try to open the file
	f, err := os.Open(cfg.TokenLogoFilePath)
	if err != nil {
		log.Printf("can not open ERC20 tokens map file; %s", err.Error())
		return
	}

	// make sure to close the file
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("config file can not be closed; %s", err.Error())
		}
	}()

	// inform about tokens loading
	log.Printf("loading ERC20 tokens from %s", cfg.TokenLogoFilePath)

	// read the whole file
	data, err := ioutil.ReadAll(f)
	if err != nil {
		log.Printf("can not read ERC20 tokens map file; %s", err.Error())
		return
	}

	// try to unmarshal the data
	if err := json.Unmarshal(data, &cfg.TokenLogo); err != nil {
		log.Printf("can not decode ERC20 tokens map file; %s", err.Error())
		return
	}

	// inform about tokens
	log.Printf("found %d ERC20 tokens", len(cfg.TokenLogo))
}

// setupConfigUnmarshaler configures the Config loader to properly unmarshal
// special types we use for the API server
func setupConfigUnmarshaler(cfg *mapstructure.DecoderConfig) {
	// add the decoders missing here
	cfg.DecodeHook = mapstructure.ComposeDecodeHookFunc(
		StringToAddressHookFunc(),
		StringToPrivateKeyHookFunc(),
		cfg.DecodeHook)
}

// StringToPrivateKeyHookFunc returns a DecodeHookFunc that converts
// strings to mldsa87.PrivateKey type on config loading.
func StringToPrivateKeyHookFunc() mapstructure.DecodeHookFuncType {
	// return the decoder function
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		// make sure the input is expected String
		if f.Kind() != reflect.String {
			return data, nil
		}

		// make sure the output is expected to be mldsa87.PrivateKey
		if t != reflect.TypeOf(mldsa87.PrivateKey{}) {
			return data, nil
		}

		// convert input to string
		raw := data.(string)
		if raw == "" {
			return nil, fmt.Errorf("empty private key content")
		}

		// try to decode the key
		key, err := cryptod.ToMLDsa87(common.FromHex(raw))
		if err != nil {
			return nil, err
		}
		return *key, nil
	}
}

// StringToAddressHookFunc returns a DecodeHookFunc that converts
// strings to common.Address type on config loading.
func StringToAddressHookFunc() mapstructure.DecodeHookFuncType {
	// return the decoder function
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		// make sure the input is expected String
		if f.Kind() != reflect.String {
			return data, nil
		}

		// make sure the output is expected common.Address
		if t == reflect.TypeOf(common.Address{}) {
			raw := data.(string)
			if raw == "" {
				return common.HexToAddress(EmptyAddress), nil
			}
			return stringToCommonAddress(raw)
		}

		// typed address is expected here?
		if t == reflect.TypeOf(common.Address{}) {
			raw := data.(string)
			if raw == "" {
				return stringToAddress(EmptyAddress)
			}
			return stringToAddress(raw)
		}

		// anything found else?
		return data, nil
	}
}

// stringToCommonAddress converts the given String to common Address.
func stringToCommonAddress(str string) (interface{}, error) {
	return common.HexToAddress(str), nil
}

// stringToAddress converts the given String to typed Address.
func stringToAddress(str string) (interface{}, error) {
	return common.HexToAddress(str), nil
}

// reader provides instance of the config reader.
// It accepts an explicit path to a config file if it was requested by `cfg` flag.
func reader() *viper.Viper {
	cfg := viper.New()

	// Set the default config file name (no extension)
	cfg.SetConfigName(configFileName)

	// Add config search paths
	cfg.AddConfigPath(defaultConfigDir())
	cfg.AddConfigPath(".")

	// Define flag for config file path
	var cfgPath string
	flag.StringVar(&cfgPath, keyConfigFilePath, "", "Path to a configuration file")
	flag.Parse()

	// Use explicit config file if provided
	if cfgPath != "" {
		cfg.SetConfigFile(cfgPath)
	}

	return cfg
}
