package handlers

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// ConfigSet writes one key to the chosen config layer, rejecting a key that
// does not belong in that file before anything lands on disk.
func (h *Handler) ConfigSet(ctx context.Context, cmd *command.ConfigSetCmd) error {
	var (
		file   configFile
		probe  any
		typ    reflect.Type
		layer  string
		source model.ConfigValueSource
		err    error
	)
	switch cmd.Target {
	case "global":
		file, err = h.globalConfigFile()
		probe, typ, layer, source = &repos.GlobalConfig{}, reflect.TypeFor[repos.GlobalConfig](), "global", model.ConfigSourceGlobal
	case "local":
		file, err = h.localConfigFile()
		probe, typ, layer, source = &model.PerRepoConfig{}, reflect.TypeFor[model.PerRepoConfig](), "per-repo", model.ConfigSourceLocal
	default:
		return fmt.Errorf("unknown config target %q (use global or local)", cmd.Target)
	}
	if err != nil {
		return err
	}

	return file.patch(func(existing []byte) ([]byte, error) {
		patched, err := model.SetYAMLField(existing, cmd.Key, model.ParseConfigScalar(cmd.Value))
		if err != nil {
			return nil, fmt.Errorf("setting %s: %w", cmd.Key, err)
		}
		// Judged per key rather than over the document, so a key some other
		// version wrote does not make this file unwritable.
		if !model.KnownYAMLKey(cmd.Key, typ) {
			return nil, fmt.Errorf("%q is not a valid %s config key", cmd.Key, layer)
		}
		if err := model.UnmarshalYAML(patched, probe); err != nil {
			return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
		}
		if cmd.Key == "embedding.path" {
			if err := model.ValidateEmbeddingPath(cmd.Value); err != nil {
				return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
			}
		}
		if global, ok := probe.(*repos.GlobalConfig); ok {
			var err error
			switch cmd.Key {
			case "cache.retention":
				_, err = model.ResolveCacheRetention(global.Cache)
			case "index.replaced_retention":
				_, err = model.ResolveReplacedRetention(global.Index)
			}
			if err != nil {
				return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
			}
		}
		var settings model.BaseConfig
		if err := model.UnmarshalYAML(patched, &settings); err != nil {
			return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
		}
		for _, ignored := range model.IgnoredSettings(settings, source) {
			if ignored.Key == cmd.Key || strings.HasPrefix(cmd.Key, ignored.Key+".") {
				return nil, fmt.Errorf("%s is not valid for the %s config: %s", cmd.Key, layer, ignored.Reason)
			}
		}
		return patched, nil
	})
}

// ConfigUnset removes one key from the chosen config layer. No schema check:
// removing any key — a known one or a leftover an older version wrote —
// keeps the document valid.
func (h *Handler) ConfigUnset(ctx context.Context, cmd *command.ConfigUnsetCmd) error {
	var (
		file configFile
		err  error
	)
	switch cmd.Target {
	case "global":
		file, err = h.globalConfigFile()
	case "local":
		file, err = h.localConfigFile()
	default:
		return fmt.Errorf("unknown config target %q (use global or local)", cmd.Target)
	}
	if err != nil {
		return err
	}

	return file.patch(func(existing []byte) ([]byte, error) {
		patched, err := model.DeleteYAMLField(existing, cmd.Key)
		if err != nil {
			return nil, fmt.Errorf("unsetting %s: %w", cmd.Key, err)
		}
		return patched, nil
	})
}
