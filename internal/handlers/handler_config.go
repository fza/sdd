package handlers

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/meta"
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
	if cmd.Key == "inbound" || strings.HasPrefix(cmd.Key, "inbound.") {
		return fmt.Errorf("%s is set only in the committed .sdd/config.yaml of the graph receiving captures, never in the %s config", cmd.Key, cmd.Target)
	}
	if slices.Contains(model.RetiredConfigKeys, cmd.Key) {
		return fmt.Errorf("%s is no longer used: git operations sdd starts are limited per remote host under sync.default and sync.hosts in the global config", cmd.Key)
	}
	segments := strings.Split(cmd.Key, ".")
	hostsPath, inHosts, err := model.SyncHostsKeyPath(cmd.Key)
	if err != nil {
		return err
	}
	if inHosts {
		segments = hostsPath
	}
	if cmd.Repo != "" {
		segments, err = model.RepoOverrideKey(cmd.Key, model.RepoID(cmd.Repo))
		if err != nil {
			return err
		}
	}

	return file.patch(func(existing []byte) ([]byte, error) {
		patched, err := model.SetYAMLFieldAt(existing, segments, model.ParseConfigScalar(cmd.Value))
		if err != nil {
			return nil, fmt.Errorf("setting %s: %w", cmd.Key, err)
		}
		// Judged per key rather than over the document, so a key some other
		// version wrote does not make this file unwritable.
		if !model.KnownYAMLKey(cmd.Key, typ) {
			return nil, fmt.Errorf("%q is not a valid %s config key", cmd.Key, layer)
		}
		if err := model.ConfigValueRefusal(typ, segments, cmd.Value); err != nil {
			return nil, err
		}
		if err := model.UnmarshalYAML(patched, probe); err != nil {
			return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
		}
		if global, ok := probe.(*repos.GlobalConfig); ok && cmd.Key == "cache.retention" {
			if _, err := model.ResolveCacheRetention(global.Cache); err != nil {
				return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
			}
		}
		var settings model.BaseConfig
		if err := model.UnmarshalYAML(patched, &settings); err != nil {
			return nil, fmt.Errorf("%s = %s is not valid for the %s config: %w", cmd.Key, cmd.Value, layer, err)
		}
		ignoredSettings := model.IgnoredSettings(settings, source)
		if source == model.ConfigSourceGlobal {
			ignoredSettings = append(ignoredSettings, model.IgnoredGitLimits(settings.Sync)...)
		} else {
			ignoredSettings = append(ignoredSettings, model.PerRepoIgnoredSettings(settings)...)
		}
		for _, ignored := range ignoredSettings {
			if ignored.Key == cmd.Key || strings.HasPrefix(cmd.Key, ignored.Key+".") {
				return nil, fmt.Errorf("%s = %s is not valid for the %s config: %s", cmd.Key, cmd.Value, layer, ignored.Reason)
			}
		}
		if strings.HasPrefix(cmd.Key, "verify.") {
			if err := h.verifyWriteRefusal(cmd, patched); err != nil {
				return nil, err
			}
		}
		return patched, nil
	})
}

// verifyWriteRefusal runs the verify check loading runs over the layers as
// they stand after the write, judging cmd.Key alone.
func (h *Handler) verifyWriteRefusal(cmd *command.ConfigSetCmd, patched []byte) error {
	var global model.BaseConfig
	switch {
	case cmd.Target == "global":
		cfg, err := repos.ParseGlobalConfig(patched)
		if err != nil {
			return err
		}
		global = cfg.BaseConfig
	case h.repos != nil:
		cfg, err := h.repos.Registry().Load()
		if err != nil {
			return err
		}
		global = cfg.BaseConfig
	}

	var committed, local *model.PerRepoConfig
	if h.sddDir != "" {
		file, err := h.repoConfigFile()
		if err != nil {
			return err
		}
		committed, err = readConfigLayer(file)
		if err != nil {
			return err
		}
		committed = model.HonoredCommitted(committed)
	}
	switch {
	case cmd.Target == "local":
		cfg, err := model.ParseConfig(patched)
		if err != nil {
			return err
		}
		local = model.HonoredLocal(cfg)
	case h.sddDir != "" || h.localConfigPath != "":
		file, err := h.localConfigFile()
		if err != nil {
			return err
		}
		local, err = readConfigLayer(file)
		if err != nil {
			return err
		}
		local = model.HonoredLocal(local)
	}

	return model.VerifyKeyRefusal(cmd.Key, global, committed, local, meta.ConfigLayerFiles(h.sddDir, h.localConfigPath))
}

// readConfigLayer parses one per-repo layer, nil when its file is missing.
func readConfigLayer(file configFile) (*model.PerRepoConfig, error) {
	data, err := file.read()
	if err != nil || data == nil {
		return nil, err
	}
	cfg, err := model.ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file.path, err)
	}
	return cfg, nil
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
