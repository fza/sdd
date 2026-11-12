package finders

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

// EffectiveConfig resolves the config overlay layer by layer and reports
// each effective value with the layer that supplied it. The finder reads the
// layers itself (rather than using the merged construction-time snapshot)
// because provenance is exactly the information merging discards.
func (f *Finder) EffectiveConfig(q query.EffectiveConfigQuery) (*query.EffectiveConfigResult, error) {
	var global model.BaseConfig
	if f.repos != nil {
		gcfg, err := f.repos.Load()
		if err != nil {
			return nil, err
		}
		global = gcfg.BaseConfig
	}
	var project, local *model.PerRepoConfig
	if q.SDDDir != "" {
		var err error
		project, local, err = meta.ReadConfigLayers(q.SDDDir, q.LocalConfigPath)
		if err != nil {
			return nil, err
		}
	}

	var work model.WorkConfigLayers
	if q.WorkSDDDir != "" {
		var err error
		if work.Committed, _, err = meta.ReadConfigLayers(q.WorkSDDDir, ""); err != nil {
			return nil, err
		}
		if q.WorkLocalConfigPath != "" {
			if _, work.Local, err = meta.ReadConfigLayers("", q.WorkLocalConfigPath); err != nil {
				return nil, err
			}
		}
	}

	values := model.EffectiveConfigValuesWithWork(global, work, project, local)
	result := &query.EffectiveConfigResult{}
	for _, v := range values {
		// Key selects exactly, or as a dotted-path prefix ("llm" matches
		// llm.provider); empty selects everything.
		if q.Key != "" && v.Key != q.Key && !strings.HasPrefix(v.Key, q.Key+".") {
			continue
		}
		result.Entries = append(result.Entries, query.ConfigEntry{
			Key:    v.Key,
			Value:  v.Value,
			Source: string(v.Source),
			Secret: v.Secret,
		})
	}
	return result, nil
}

// UnknownConfigKeys reports the keys sdd read past because it does not know
// them, and the settings it read past because it may not honor them where
// they stand — the one computation behind every surface that says so, rather
// than each deciding for itself what counts as unknown.
func (f *Finder) UnknownConfigKeys(q query.UnknownConfigKeysQuery) (*query.UnknownConfigKeysResult, error) {
	type layer struct {
		path   string
		typ    reflect.Type
		source model.ConfigValueSource
	}
	var layers []layer
	if f.repos != nil {
		layers = append(layers, layer{path: f.repos.ConfigPath(), typ: reflect.TypeFor[repos.GlobalConfig](), source: model.ConfigSourceGlobal})
	}
	if q.SDDDir != "" {
		perRepo := reflect.TypeFor[model.PerRepoConfig]()
		layers = append(layers,
			layer{path: filepath.Join(q.SDDDir, "config.yaml"), typ: perRepo, source: model.ConfigSourceProject},
			layer{path: meta.LocalConfigPath(q.SDDDir, q.LocalConfigPath), typ: perRepo, source: model.ConfigSourceLocal},
		)
	}

	result := &query.UnknownConfigKeysResult{}
	for _, l := range layers {
		data, err := os.ReadFile(l.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		keys, err := model.UnknownYAMLKeys(data, l.typ)
		if err != nil {
			return nil, err
		}
		var settings model.BaseConfig
		if err := model.UnmarshalYAML(data, &settings); err != nil {
			return nil, err
		}
		ignored := model.IgnoredSettings(settings, l.source)
		for _, k := range keys {
			if reason, retired := model.RetiredConfigKeys[k]; retired {
				ignored = append(ignored, model.IgnoredSetting{Key: k, Reason: "no longer used: " + reason})
				continue
			}
			result.Keys = append(result.Keys, query.UnknownConfigKey{File: l.path, Key: k})
		}
		for _, setting := range ignored {
			result.Ignored = append(result.Ignored, query.IgnoredConfigSetting{File: l.path, Key: setting.Key, Reason: setting.Reason})
		}
	}
	return result, nil
}
