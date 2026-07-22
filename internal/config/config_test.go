package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestDefaultsReachTheirStructFields is the guard for a silent failure class.
//
// Every default is registered against a viper key string, and viper later unmarshals
// into Config using `mapstructure` tags. If the key string and the tag path disagree by
// even one character, SetDefault writes a key nothing reads: no error, no warning, and
// the field keeps its zero value. The operator sees documented behaviour that does not
// happen.
//
// This was live: keyForestUrl was "forest.url" while Config.Forest is tagged
// `mapstructure:"node"`, so the default node IPC path never reached cfg.Forest.Url and
// an operator omitting node.url got an empty dial string.
//
// The test asserts the inverse property directly: apply the defaults, unmarshal into a
// Config exactly as Load() does, and require that fields with a non-empty default are
// actually populated.
func TestDefaultsReachTheirStructFields(t *testing.T) {
	v := viper.New()
	applyDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg, setupConfigUnmarshaler); err != nil {
		t.Fatalf("unmarshal defaults into Config: %v", err)
	}

	// Fields that carry a meaningful default and must therefore be non-zero after
	// unmarshalling. Each names the key whose default feeds it, so a failure points
	// straight at the mismatched constant.
	cases := []struct {
		field string
		key   string
		got   func() interface{}
	}{
		{"Forest.Url", keyForestUrl, func() interface{} { return cfg.Forest.Url }},
		{"Db.Url", keyMongoUrl, func() interface{} { return cfg.Db.Url }},
		{"Db.DbName", keyMongoDatabase, func() interface{} { return cfg.Db.DbName }},
		{"Server.BindAddress", keyBindAddress, func() interface{} { return cfg.Server.BindAddress }},
		{"Server.DomainAddress", keyDomainAddress, func() interface{} { return cfg.Server.DomainAddress }},
		{"AppName", keyAppName, func() interface{} { return cfg.AppName }},
		{"Compiler.DefaultSolCompilerPath", keySolCompilerPath, func() interface{} { return cfg.Compiler.DefaultSolCompilerPath }},
		{"Compiler.CompilerTempPath", keyCompilerTempPath, func() interface{} { return cfg.Compiler.CompilerTempPath }},
		{"Server.MaxQueryDepth", keyMaxQueryDepth, func() interface{} { return cfg.Server.MaxQueryDepth }},
		{"Server.MaxRequestBody", keyMaxRequestBody, func() interface{} { return cfg.Server.MaxRequestBody }},
		{"Server.MaxQueryComplexity", keyMaxQueryComplex, func() interface{} { return cfg.Server.MaxQueryComplexity }},
		{"Server.MaxParallelism", keyMaxParallelism, func() interface{} { return cfg.Server.MaxParallelism }},
	}

	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			if !v.IsSet(c.key) {
				t.Fatalf("no default registered for key %q", c.key)
			}
			got := c.got()
			if isZero(got) {
				t.Errorf("Config.%s is zero after unmarshalling defaults.\n"+
					"The default is registered at key %q, but that key does not match the "+
					"mapstructure path of this field, so the default is inert.",
					c.field, c.key)
			}
		})
	}
}

// TestConfigKeysMatchStructPaths walks Config's mapstructure tags and checks that every
// key constant used with SetDefault addresses a path that actually exists.
//
// This catches the mismatch from the other direction: a key whose prefix names no field
// at all (e.g. "forest.*" when the struct exposes "node").
func TestConfigKeysMatchStructPaths(t *testing.T) {
	paths := map[string]bool{}
	collectPaths(reflect.TypeOf(Config{}), "", paths)

	// Keys that intentionally do not map onto a Config field. Each needs a reason.
	exempt := map[string]string{
		keyConfigFilePath: "a CLI flag for locating the config file, not a config value",
		keyErc20Logos:     "loaded into TokenLogo by hand in load.go; the field is untagged",
		keyVotingSources:  "no corresponding field; retained for backwards compatibility",
	}

	keys := map[string]string{
		"keyAppName":            keyAppName,
		"keyBindAddress":        keyBindAddress,
		"keyDomainAddress":      keyDomainAddress,
		"keyApiPeers":           keyApiPeers,
		"keyApiStateOrigin":     keyApiStateOrigin,
		"keyCorsAllowOrigins":   keyCorsAllowOrigins,
		"keyTimeoutRead":        keyTimeoutRead,
		"keyTimeoutWrite":       keyTimeoutWrite,
		"keyTimeoutIdle":        keyTimeoutIdle,
		"keyTimeoutHeader":      keyTimeoutHeader,
		"keyTimeoutResolver":    keyTimeoutResolver,
		"keyMaxQueryDepth":      keyMaxQueryDepth,
		"keyMaxQueryComplex":    keyMaxQueryComplex,
		"keyMaxRequestBody":     keyMaxRequestBody,
		"keyMaxParallelism":     keyMaxParallelism,
		"keyGraphiEnabled":      keyGraphiEnabled,
		"keySignatureAddress":   keySignatureAddress,
		"keyLoggingLevel":       keyLoggingLevel,
		"keyLoggingFormat":      keyLoggingFormat,
		"keyForestUrl":          keyForestUrl,
		"keyMongoUrl":           keyMongoUrl,
		"keyMongoDatabase":      keyMongoDatabase,
		"keyCacheEvictionTime":  keyCacheEvictionTime,
		"keyCacheMaxSize":       keyCacheMaxSize,
		"keySolCompilerPath":    keySolCompilerPath,
		"keyCompilerTempPath":   keyCompilerTempPath,
		"keyErc20TokenMapFilePath": keyErc20TokenMapFilePath,
	}

	for name, key := range keys {
		if _, ok := exempt[key]; ok {
			continue
		}
		if !paths[strings.ToLower(key)] {
			t.Errorf("%s = %q addresses no mapstructure path in Config; "+
				"any default registered under it is silently inert", name, key)
		}
	}
}

// collectPaths records every dotted mapstructure path reachable in t.
func collectPaths(t reflect.Type, prefix string, out map[string]bool) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)

		tag := f.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		tag = strings.Split(tag, ",")[0]

		path := tag
		if prefix != "" {
			path = prefix + "." + tag
		}
		out[strings.ToLower(path)] = true

		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			collectPaths(ft, path, out)
		}
	}
}

func isZero(v interface{}) bool {
	if v == nil {
		return true
	}
	return reflect.ValueOf(v).IsZero()
}
